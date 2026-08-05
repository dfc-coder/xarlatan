// Package llm manages a llama-server subprocess and communicates via its
// OpenAI-compatible REST API with full tool/function-calling support.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/tools"
)

// ─── Typed message (replaces map[string]any) ─────────────────────────────────

// Message is a single conversation turn. Fields are omitted when empty so the
// JSON matches what llama-server expects for each role variant.
type Message struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []tools.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// ─── Wire types ───────────────────────────────────────────────────────────────

type chatRequest struct {
	Messages    []Message          `json:"messages"`
	Tools       []tools.Definition `json:"tools,omitempty"`
	ToolChoice  string             `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature"`
	TopP        float64            `json:"top_p"`
	MaxTokens   int                `json:"max_tokens"`
	Stream      bool               `json:"stream"`
}

// streamChunk represents one SSE delta from /v1/chat/completions.
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
					Arguments string `json:"arguments"` // arrives as string fragments
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// ─── Client ───────────────────────────────────────────────────────────────────

type Client struct {
	cfg    config.LLMConfig
	http   *http.Client
	server *exec.Cmd
}

func New(cfg config.LLMConfig) (*Client, error) {
	c := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 180 * time.Second},
	}
	return c, c.startServer()
}

func (c *Client) startServer() error {
	args := []string{
		"--model", c.cfg.Model,
		"--host", c.cfg.Host,
		"--port", fmt.Sprintf("%d", c.cfg.Port),
		"--ctx-size", fmt.Sprintf("%d", c.cfg.ContextSize),
		"--n-gpu-layers", fmt.Sprintf("%d", c.cfg.NGPULayers),
		"--threads", fmt.Sprintf("%d", c.cfg.Threads),
		"--log-disable",
	}
	slog.Info("Starting llama-server", "model", c.cfg.Model)
	cmd := exec.Command(c.cfg.ServerBinary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("llama-server: %w", err)
	}
	c.server = cmd

	health := c.cfg.BaseURL() + "/health"
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); {
		if resp, err := c.http.Get(health); err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			slog.Info("llama-server ready", "url", c.cfg.BaseURL())
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("llama-server timeout")
}

// Generate runs a single LLM turn and returns the raw response, tool calls,
// and the next conversation history.
func (c *Client) Generate(ctx context.Context, history []Message, userText string, registry *tools.Registry) (string, string, []Message, error) {
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
	req := chatRequest{
		Messages:    history,
		Temperature: c.cfg.Temperature,
		TopP:        c.cfg.TopP,
		MaxTokens:   c.cfg.MaxTokens,
		Stream:      true,
	}
	if registry != nil {
		if defs := registry.Definitions(); len(defs) > 0 {
			req.Tools = defs
			req.ToolChoice = "auto"
		}
	}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL()+"/v1/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("llama-server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("llama-server %d: %s", resp.StatusCode, b)
	}
	return parseStream(resp.Body)
}

// parseStream reads SSE and reassembles streamed tool-call argument fragments.
func parseStream(r io.Reader) (content string, calls []tools.ToolCall, err error) {
	// argBuf accumulates tool-call argument fragments keyed by delta index.
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

		for _, tc := range delta.ToolCalls {
			for len(partials) <= tc.Index {
				partials = append(partials, partial{})
			}
			p := &partials[tc.Index]
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Type != "" {
				p.typ = tc.Type
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args.WriteString(tc.Function.Arguments)
		}
	}
	if e := scanner.Err(); e != nil {
		return "", nil, fmt.Errorf("stream: %w", e)
	}

	calls = make([]tools.ToolCall, 0, len(partials))
	for _, p := range partials {
		args := p.args.String()
		if args == "" {
			args = "{}"
		}
		calls = append(calls, tools.ToolCall{
			ID:   p.id,
			Type: p.typ,
			Function: tools.CallFunction{
				Name:      p.name,
				Arguments: json.RawMessage(args),
			},
		})
	}
	return content, calls, nil
}

func (c *Client) Close() error {
	if c.server != nil && c.server.Process != nil {
		_ = c.server.Process.Kill()
		return c.server.Wait()
	}
	return nil
}
