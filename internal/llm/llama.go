// Package llm communicates with an OpenAI-compatible language-model endpoint
// and manages an optional llama-server subprocess through a separate lifecycle.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dfc-coder/xarlatan/internal/tools"
)

// Message is a single conversation turn. Fields are omitted when empty so the
// JSON matches what llama-server expects for each role variant.
type Message struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []tools.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type chatRequest struct {
	Messages    []Message          `json:"messages"`
	Tools       []tools.Definition `json:"tools,omitempty"`
	ToolChoice  string             `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature"`
	TopP        float64            `json:"top_p"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// HTTPDoer is the minimal HTTP boundary used by the client and health probe.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// ClientConfig configures inference only. Process ownership belongs to
// ServerManager and is deliberately absent from this contract.
type ClientConfig struct {
	BaseURL     string
	Temperature float64
	TopP        float64
	MaxTokens   int
	HTTPClient  HTTPDoer
}

type requestFactory func(context.Context, string, string, []byte) (*http.Request, error)
type jsonMarshal func(any) ([]byte, error)

// ClientOption customizes testable client boundaries.
type ClientOption func(*Client)

func withJSONMarshal(marshal jsonMarshal) ClientOption {
	return func(c *Client) { c.marshal = marshal }
}

func withRequestFactory(factory requestFactory) ClientOption {
	return func(c *Client) { c.newRequest = factory }
}

// Client sends inference requests. It owns no subprocess lifecycle.
type Client struct {
	baseURL     *url.URL
	temperature float64
	topP        float64
	maxTokens   int
	http        HTTPDoer
	marshal     jsonMarshal
	newRequest  requestFactory
}

// NewClient validates and constructs an inference client without spawning a
// server. A managed or external server must be prepared independently.
func NewClient(cfg ClientConfig, options ...ClientOption) (*Client, error) {
	baseURL, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("llm client base URL: %w", err)
	}
	if cfg.Temperature < 0 || cfg.Temperature > 2 {
		return nil, fmt.Errorf("llm client temperature must be between 0 and 2")
	}
	if cfg.TopP <= 0 || cfg.TopP > 1 {
		return nil, fmt.Errorf("llm client top_p must be greater than 0 and at most 1")
	}
	if cfg.MaxTokens <= 0 {
		return nil, fmt.Errorf("llm client max_tokens must be greater than zero")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 180 * time.Second}
	}
	client := &Client{
		baseURL:     baseURL,
		temperature: cfg.Temperature,
		topP:        cfg.TopP,
		maxTokens:   cfg.MaxTokens,
		http:        httpClient,
		marshal:     json.Marshal,
		newRequest:  defaultRequestFactory,
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	if client.marshal == nil {
		return nil, fmt.Errorf("llm client marshal function is nil")
	}
	if client.newRequest == nil {
		return nil, fmt.Errorf("llm client request factory is nil")
	}
	return client, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("userinfo is not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("query and fragment are not allowed")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, fmt.Errorf("base URL path must be empty")
	}
	parsed.Path = ""
	return parsed, nil
}

func defaultRequestFactory(ctx context.Context, method, target string, body []byte) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
}

// Generate runs a single model call and returns the response, tool-call log,
// and next immutable conversation history.
func (c *Client) Generate(ctx context.Context, history []Message, userText string, registry *tools.Registry) (string, string, []Message, error) {
	if c == nil {
		return "", "", nil, fmt.Errorf("llm client is nil")
	}
	nextHistory := append([]Message(nil), history...)
	if strings.TrimSpace(userText) != "" {
		nextHistory = append(nextHistory, Message{Role: "user", Content: userText})
	}

	content, calls, err := c.callLLM(ctx, nextHistory, registry)
	if err != nil {
		return "", "", nil, err
	}
	if len(calls) == 0 {
		reply := strings.TrimSpace(content)
		nextHistory = append(nextHistory, Message{Role: "assistant", Content: reply})
		return reply, "", nextHistory, nil
	}

	nextHistory = append(nextHistory, Message{Role: "assistant", Content: content, ToolCalls: calls})
	return content, tools.FormatToolCalls(calls), nextHistory, nil
}

func (c *Client) callLLM(ctx context.Context, history []Message, registry *tools.Registry) (content string, calls []tools.ToolCall, err error) {
	reqPayload := chatRequest{
		Messages:    history,
		Temperature: c.temperature,
		TopP:        c.topP,
		MaxTokens:   c.maxTokens,
		Stream:      true,
	}
	if registry != nil {
		if definitions := registry.Definitions(); len(definitions) > 0 {
			reqPayload.Tools = definitions
			reqPayload.ToolChoice = "auto"
		}
	}

	body, err := c.marshal(reqPayload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal llama-server request: %w", err)
	}
	endpoint := *c.baseURL
	endpoint.Path = "/v1/chat/completions"
	httpReq, err := c.newRequest(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return "", nil, fmt.Errorf("create llama-server request: %w", err)
	}
	if httpReq == nil {
		return "", nil, fmt.Errorf("create llama-server request: nil request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("llama-server request: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return "", nil, fmt.Errorf("llama-server response has no body")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if readErr != nil {
			return "", nil, fmt.Errorf("llama-server status %d: read body: %w", resp.StatusCode, readErr)
		}
		return "", nil, fmt.Errorf("llama-server status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return parseStream(resp.Body)
}

// parseStream reads SSE and reassembles streamed tool-call argument fragments.
func parseStream(r io.Reader) (content string, calls []tools.ToolCall, err error) {
	type partial struct {
		id, typ, name string
		args          strings.Builder
	}
	var partials []partial

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		content += delta.Content
		for _, toolCall := range delta.ToolCalls {
			for len(partials) <= toolCall.Index {
				partials = append(partials, partial{})
			}
			current := &partials[toolCall.Index]
			if toolCall.ID != "" {
				current.id = toolCall.ID
			}
			if toolCall.Type != "" {
				current.typ = toolCall.Type
			}
			if toolCall.Function.Name != "" {
				current.name = toolCall.Function.Name
			}
			current.args.WriteString(toolCall.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, fmt.Errorf("stream: %w", err)
	}

	calls = make([]tools.ToolCall, 0, len(partials))
	for _, current := range partials {
		arguments := current.args.String()
		if arguments == "" {
			arguments = "{}"
		}
		calls = append(calls, tools.ToolCall{
			ID:   current.id,
			Type: current.typ,
			Function: tools.CallFunction{
				Name:      current.name,
				Arguments: json.RawMessage(arguments),
			},
		})
	}
	return content, calls, nil
}
