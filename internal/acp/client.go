package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	ErrProtocolVersion = errors.New("ACP protocol version mismatch")
	ErrSessionBusy     = errors.New("ACP session busy")
	ErrClosed          = errors.New("ACP client closed")
)

// ClientConfig contains the portable ACP session settings owned by Xarlatan.
type ClientConfig struct {
	CWD string
}

// AgentInfo is optional metadata advertised by the ACP server at initialize.
type AgentInfo struct {
	Name    string
	Title   string
	Version string
}

// Result is the terminal user-facing outcome of one ACP prompt turn.
type Result struct {
	Reply      string
	StopReason string
}

// Client is a single-session ACP v1 client. Xarlatan serializes prompts for a
// voice session, so at most one prompt is active at a time.
type Client struct {
	reader *bufio.Reader
	writer io.Writer
	cfg    ClientConfig

	writeMu sync.Mutex
	stateMu sync.Mutex
	closed  bool
	session string
	info    AgentInfo

	promptBusy atomic.Bool
	nextID     atomic.Uint64
	closeOnce  sync.Once
	closeErr   error
}

// NewClient builds an ACP client over newline-delimited JSON-RPC streams.
func NewClient(reader io.Reader, writer io.Writer, cfg ClientConfig) (*Client, error) {
	if reader == nil {
		return nil, errors.New("ACP reader is nil")
	}
	if writer == nil {
		return nil, errors.New("ACP writer is nil")
	}
	cfg.CWD = strings.TrimSpace(cfg.CWD)
	if cfg.CWD == "" {
		return nil, errors.New("ACP cwd is required")
	}
	if !filepath.IsAbs(cfg.CWD) {
		return nil, errors.New("ACP cwd must be an absolute path")
	}
	return &Client{reader: bufio.NewReader(reader), writer: writer, cfg: cfg}, nil
}

// Initialize performs the ACP v1 handshake and creates exactly one session for
// the lifetime of this client.
func (c *Client) Initialize(ctx context.Context) error {
	if c == nil {
		return errors.New("ACP client is nil")
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return ErrClosed
	}

	id := c.requestID()
	if err := c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params":  map[string]any{"protocolVersion": 1},
	}); err != nil {
		return err
	}
	message, err := c.readResponse(ctx, id)
	if err != nil {
		return err
	}
	result, err := responseResult(message)
	if err != nil {
		return err
	}
	version, ok := numberAsInt(result["protocolVersion"])
	if !ok || version != 1 {
		return fmt.Errorf("%w: server=%v want=1", ErrProtocolVersion, result["protocolVersion"])
	}
	info := parseAgentInfo(result["agentInfo"])

	id = c.requestID()
	if err := c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/new",
		"params": map[string]any{
			"cwd": c.cfg.CWD,
		},
	}); err != nil {
		return err
	}
	message, err = c.readResponse(ctx, id)
	if err != nil {
		return err
	}
	result, err = responseResult(message)
	if err != nil {
		return err
	}
	sessionID, _ := result["sessionId"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("ACP session/new returned empty sessionId")
	}

	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return ErrClosed
	}
	c.session = sessionID
	c.info = info
	c.stateMu.Unlock()
	return nil
}

// AgentInfo returns a copy of the bounded metadata advertised at initialize.
func (c *Client) AgentInfo() AgentInfo {
	if c == nil {
		return AgentInfo{}
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.info
}

// RespondStream submits an authoritative text transcript and forwards only
// user-facing agent_message_chunk text. Plans, thoughts and tool payloads are
// deliberately not voice-eligible.
func (c *Client) RespondStream(ctx context.Context, prompt string, onDelta func(string) error) (Result, error) {
	if c == nil {
		return Result{}, errors.New("ACP client is nil")
	}
	ctx = nonNilContext(ctx)
	if !c.promptBusy.CompareAndSwap(false, true) {
		return Result{}, ErrSessionBusy
	}
	defer c.promptBusy.Store(false)

	sessionID, err := c.sessionID()
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	id := c.requestID()
	if err := c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt": []any{
				map[string]any{"type": "text", "text": prompt},
			},
		},
	}); err != nil {
		return Result{}, err
	}

	cancelDone := make(chan struct{})
	defer close(cancelDone)
	var cancelled atomic.Bool
	go func() {
		select {
		case <-cancelDone:
			return
		case <-ctx.Done():
			cancelled.Store(true)
			_ = c.write(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/cancel",
				"params":  map[string]any{"sessionId": sessionID},
			})
		}
	}()

	var streamed strings.Builder
	for {
		message, err := c.read()
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			return Result{}, err
		}

		if method, _ := message["method"].(string); method != "" {
			switch method {
			case "session/update":
				if cancelled.Load() || ctx.Err() != nil {
					continue
				}
				delta := voiceDelta(message, sessionID)
				if delta == "" {
					continue
				}
				streamed.WriteString(delta)
				if onDelta != nil {
					if err := onDelta(delta); err != nil {
						return Result{}, err
					}
				}
			case "session/request_permission":
				if err := c.rejectPermission(message); err != nil {
					return Result{}, err
				}
			}
			continue
		}

		if !sameID(message["id"], id) {
			continue
		}
		result, err := responseResult(message)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			return Result{}, err
		}
		if cancelled.Load() || ctx.Err() != nil {
			return Result{}, context.Canceled
		}

		reply, _ := result["content"].(string)
		if strings.TrimSpace(reply) == "" {
			reply = streamed.String()
		}
		stopReason, _ := result["stopReason"].(string)
		return Result{Reply: strings.TrimSpace(reply), StopReason: stopReason}, nil
	}
}

func (c *Client) rejectPermission(message map[string]any) error {
	id, exists := message["id"]
	if !exists || id == nil {
		return nil
	}
	optionID := rejectOptionID(message)
	outcome := map[string]any{"outcome": "cancelled"}
	if optionID != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": optionID}
	}
	return c.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  map[string]any{"outcome": outcome},
	})
}

func rejectOptionID(message map[string]any) string {
	params, _ := message["params"].(map[string]any)
	options, _ := params["options"].([]any)
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		kind, _ := option["kind"].(string)
		if strings.HasPrefix(strings.ToLower(kind), "reject") {
			id, _ := option["optionId"].(string)
			return id
		}
	}
	return ""
}

func voiceDelta(message map[string]any, sessionID string) string {
	params, _ := message["params"].(map[string]any)
	if got, _ := params["sessionId"].(string); got != sessionID {
		return ""
	}
	update, _ := params["update"].(map[string]any)
	kind, _ := update["sessionUpdate"].(string)
	if kind != "agent_message_chunk" {
		return ""
	}
	content, _ := update["content"].(map[string]any)
	if contentType, _ := content["type"].(string); contentType != "text" {
		return ""
	}
	text, _ := content["text"].(string)
	return text
}

func parseAgentInfo(value any) AgentInfo {
	raw, _ := value.(map[string]any)
	name, _ := raw["name"].(string)
	title, _ := raw["title"].(string)
	version, _ := raw["version"].(string)
	return AgentInfo{
		Name:    strings.TrimSpace(name),
		Title:   strings.TrimSpace(title),
		Version: strings.TrimSpace(version),
	}
}

func (c *Client) readResponse(ctx context.Context, id uint64) (map[string]any, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		message, err := c.read()
		if err != nil {
			return nil, err
		}
		if method, _ := message["method"].(string); method == "session/request_permission" {
			if err := c.rejectPermission(message); err != nil {
				return nil, err
			}
			continue
		}
		if sameID(message["id"], id) {
			return message, nil
		}
	}
}

func (c *Client) read() (map[string]any, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("read ACP: %w", err)
	}
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return nil, fmt.Errorf("decode ACP: %w", err)
	}
	return message, nil
}

func (c *Client) write(message any) error {
	if c.isClosed() {
		return ErrClosed
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode ACP: %w", err)
	}
	payload = append(payload, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.writer.Write(payload); err != nil {
		return fmt.Errorf("write ACP: %w", err)
	}
	return nil
}

func (c *Client) requestID() uint64 {
	return c.nextID.Add(1)
}

func (c *Client) sessionID() (string, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return "", ErrClosed
	}
	if c.session == "" {
		return "", errors.New("ACP client is not initialized")
	}
	return c.session, nil
}

func (c *Client) isClosed() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.closed
}

// Close closes owned stream endpoints when they implement io.Closer. It is
// safe to call multiple times.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.stateMu.Lock()
		c.closed = true
		c.stateMu.Unlock()

		var errs []error
		if closer, ok := c.writer.(io.Closer); ok {
			if err := closer.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				errs = append(errs, err)
			}
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

func responseResult(message map[string]any) (map[string]any, error) {
	if rawErr, ok := message["error"]; ok && rawErr != nil {
		return nil, fmt.Errorf("ACP error: %v", rawErr)
	}
	result, ok := message["result"].(map[string]any)
	if !ok {
		return nil, errors.New("ACP response missing result")
	}
	return result, nil
}

func sameID(value any, want uint64) bool {
	got, ok := numberAsInt(value)
	return ok && got == int(want)
}

func numberAsInt(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), number == float64(int(number))
	case int:
		return number, true
	case uint64:
		return int(number), true
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
