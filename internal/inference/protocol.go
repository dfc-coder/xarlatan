package inference

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

type frameHeader struct {
	ID              uint64 `json:"id"`
	Op              string `json:"op,omitempty"`
	OK              bool   `json:"ok,omitempty"`
	Error           string `json:"error,omitempty"`
	PayloadBytes    int    `json:"payload_bytes,omitempty"`
	Text            string `json:"text,omitempty"`
	SampleRate      int    `json:"sample_rate,omitempty"`
	Channels        int    `json:"channels,omitempty"`
	RequestedDevice string `json:"requested_device,omitempty"`
	ResolvedDevice  string `json:"resolved_device,omitempty"`
	FallbackReason  string `json:"fallback_reason,omitempty"`
}

// Health reports the requested and effective acceleration device.
type Health struct {
	RequestedDevice string
	ResolvedDevice  string
	FallbackReason  string
}

// Client implements the bounded metadata + raw PCM data-plane protocol used by
// the persistent OpenVINO worker. Requests are intentionally serialized.
type Client struct {
	reader *bufio.Reader
	writer io.Writer
	closer io.Closer

	mu        sync.Mutex
	nextID    atomic.Uint64
	closeOnce sync.Once
	closeErr  error
}

func NewClient(reader io.Reader, writer io.Writer) (*Client, error) {
	if reader == nil {
		return nil, errors.New("inference reader is nil")
	}
	if writer == nil {
		return nil, errors.New("inference writer is nil")
	}
	client := &Client{reader: bufio.NewReader(reader), writer: writer}
	if closer, ok := writer.(io.Closer); ok {
		client.closer = closer
	}
	return client, nil
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	response, _, err := c.call(ctx, frameHeader{Op: "health"}, nil)
	if err != nil {
		return Health{}, err
	}
	return Health{
		RequestedDevice: response.RequestedDevice,
		ResolvedDevice:  response.ResolvedDevice,
		FallbackReason:  response.FallbackReason,
	}, nil
}

func (c *Client) Transcribe(ctx context.Context, buffer audio.Buffer) (string, error) {
	if err := buffer.Validate(); err != nil {
		return "", err
	}
	if buffer.SampleRate != 16000 {
		return "", fmt.Errorf("OpenVINO STT requires 16000 Hz audio, got %d", buffer.SampleRate)
	}
	response, _, err := c.call(ctx, frameHeader{
		Op:         "transcribe",
		SampleRate: buffer.SampleRate,
		Channels:   buffer.Channels,
	}, marshalFloat32(buffer.Samples))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(response.Text), nil
}

func (c *Client) Synthesize(ctx context.Context, text string) (audio.Buffer, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return audio.Buffer{}, nil
	}
	response, payload, err := c.call(ctx, frameHeader{Op: "synthesize", Text: text}, nil)
	if err != nil {
		return audio.Buffer{}, err
	}
	if response.SampleRate <= 0 {
		return audio.Buffer{}, errors.New("inference synthesis response missing sample rate")
	}
	if response.Channels != 1 {
		return audio.Buffer{}, fmt.Errorf("inference synthesis returned %d channels, want mono", response.Channels)
	}
	samples, err := unmarshalFloat32(payload)
	if err != nil {
		return audio.Buffer{}, err
	}
	return audio.Buffer{Samples: samples, SampleRate: response.SampleRate, Channels: 1}, nil
}

func (c *Client) call(ctx context.Context, request frameHeader, payload []byte) (frameHeader, []byte, error) {
	if c == nil {
		return frameHeader{}, nil, errors.New("inference client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return frameHeader{}, nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return frameHeader{}, nil, err
	}
	request.ID = c.nextID.Add(1)
	request.PayloadBytes = len(payload)
	encoded, err := json.Marshal(request)
	if err != nil {
		return frameHeader{}, nil, fmt.Errorf("encode inference request: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := writeAll(c.writer, encoded); err != nil {
		return frameHeader{}, nil, fmt.Errorf("write inference header: %w", err)
	}
	if len(payload) > 0 {
		if err := writeAll(c.writer, payload); err != nil {
			return frameHeader{}, nil, fmt.Errorf("write inference payload: %w", err)
		}
	}

	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return frameHeader{}, nil, fmt.Errorf("read inference header: %w", err)
	}
	var response frameHeader
	if err := json.Unmarshal(line, &response); err != nil {
		return frameHeader{}, nil, fmt.Errorf("decode inference response: %w", err)
	}
	if response.ID != request.ID {
		return frameHeader{}, nil, fmt.Errorf("stale inference response id %d, want %d", response.ID, request.ID)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "worker request failed"
		}
		return frameHeader{}, nil, errors.New(response.Error)
	}
	if response.PayloadBytes < 0 {
		return frameHeader{}, nil, errors.New("negative inference payload length")
	}
	responsePayload := make([]byte, response.PayloadBytes)
	if _, err := io.ReadFull(c.reader, responsePayload); err != nil {
		return frameHeader{}, nil, fmt.Errorf("read inference payload: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return frameHeader{}, nil, err
	}
	return response, responsePayload, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.closer != nil {
			c.closeErr = c.closer.Close()
		}
	})
	return c.closeErr
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func marshalFloat32(samples []float32) []byte {
	data := make([]byte, len(samples)*4)
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(sample))
	}
	return data
}

func unmarshalFloat32(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("float32 payload length %d is not divisible by 4", len(data))
	}
	samples := make([]float32, len(data)/4)
	for i := range samples {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return samples, nil
}
