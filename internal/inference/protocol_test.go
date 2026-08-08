package inference

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestClientTranscribeUsesRawFloat32Payload(t *testing.T) {
	client, server := newProtocolPair(t)
	done := make(chan error, 1)
	go func() {
		header, payload, err := server.readFrame()
		if err != nil {
			done <- err
			return
		}
		if header.Op != "transcribe" || header.SampleRate != 16000 || header.Channels != 1 {
			done <- io.ErrUnexpectedEOF
			return
		}
		got := decodeFloat32(payload)
		if !reflect.DeepEqual(got, []float32{0.25, -0.5, 1}) {
			done <- io.ErrUnexpectedEOF
			return
		}
		done <- server.writeFrame(frameHeader{ID: header.ID, OK: true, Text: "hola mundo"}, nil)
	}()

	text, err := client.Transcribe(context.Background(), audio.Buffer{
		Samples:    []float32{0.25, -0.5, 1},
		SampleRate: 16000,
		Channels:   1,
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if text != "hola mundo" {
		t.Fatalf("text = %q", text)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestClientSynthesizeReturnsRawFloat32Audio(t *testing.T) {
	client, server := newProtocolPair(t)
	done := make(chan error, 1)
	go func() {
		header, payload, err := server.readFrame()
		if err != nil {
			done <- err
			return
		}
		if header.Op != "synthesize" || header.Text != "Hola" || len(payload) != 0 {
			done <- io.ErrUnexpectedEOF
			return
		}
		done <- server.writeFrame(frameHeader{
			ID:         header.ID,
			OK:         true,
			SampleRate: 24000,
			Channels:   1,
		}, encodeFloat32([]float32{0.1, -0.2, 0.3}))
	}()

	buffer, err := client.Synthesize(context.Background(), "Hola")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if buffer.SampleRate != 24000 || buffer.Channels != 1 || !reflect.DeepEqual(buffer.Samples, []float32{0.1, -0.2, 0.3}) {
		t.Fatalf("buffer = %+v", buffer)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestClientHealthReportsResolvedDeviceAndFallback(t *testing.T) {
	client, server := newProtocolPair(t)
	done := make(chan error, 1)
	go func() {
		header, _, err := server.readFrame()
		if err != nil {
			done <- err
			return
		}
		done <- server.writeFrame(frameHeader{
			ID:              header.ID,
			OK:              true,
			RequestedDevice: "GPU",
			ResolvedDevice:  "CPU",
			FallbackReason:  "GPU unavailable",
		}, nil)
	}()

	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.RequestedDevice != "GPU" || health.ResolvedDevice != "CPU" || health.FallbackReason == "" {
		t.Fatalf("health = %+v", health)
	}
	if err := <-done; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestClientRejectsInvalidTranscriptionAudioBeforeWrite(t *testing.T) {
	client, _ := newProtocolPair(t)
	_, err := client.Transcribe(context.Background(), audio.Buffer{Samples: []float32{1}, SampleRate: 8000, Channels: 1})
	if err == nil {
		t.Fatal("Transcribe() error = nil")
	}
}

type protocolServer struct {
	reader *bufio.Reader
	writer *io.PipeWriter
}

func newProtocolPair(t *testing.T) (*Client, *protocolServer) {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	client, err := NewClient(clientReads, clientWrites)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverWrites.Close()
		_ = serverReads.Close()
	})
	return client, &protocolServer{reader: bufio.NewReader(serverReads), writer: serverWrites}
}

func (s *protocolServer) readFrame() (frameHeader, []byte, error) {
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return frameHeader{}, nil, err
	}
	var header frameHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return frameHeader{}, nil, err
	}
	payload := make([]byte, header.PayloadBytes)
	if _, err := io.ReadFull(s.reader, payload); err != nil {
		return frameHeader{}, nil, err
	}
	return header, payload, nil
}

func (s *protocolServer) writeFrame(header frameHeader, payload []byte) error {
	header.PayloadBytes = len(payload)
	data, err := json.Marshal(header)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := s.writer.Write(data); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err = s.writer.Write(payload)
	}
	return err
}

func encodeFloat32(samples []float32) []byte {
	data := make([]byte, len(samples)*4)
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(sample))
	}
	return data
}

func decodeFloat32(data []byte) []float32 {
	samples := make([]float32, len(data)/4)
	for i := range samples {
		samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return samples
}
