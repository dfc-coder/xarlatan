package zeroclaw

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/llm"
)

type promptClientStub struct {
	result Result
	deltas []string
	err    error
	prompt string
}

func (s *promptClientStub) RespondStream(ctx context.Context, prompt string, onDelta func(string) error) (Result, error) {
	s.prompt = prompt
	if s.err != nil {
		return Result{}, s.err
	}
	for _, delta := range s.deltas {
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return Result{}, err
			}
		}
	}
	return s.result, nil
}

func TestVoiceResponderBufferedUsesZeroClawFinalReply(t *testing.T) {
	transport := &promptClientStub{result: Result{Reply: " respuesta final ", StopReason: "end_turn"}}
	responder, err := NewVoiceResponder(transport)
	if err != nil {
		t.Fatalf("NewVoiceResponder() error = %v", err)
	}
	result, err := responder.Respond(context.Background(), "hola")
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if transport.prompt != "hola" || result.Reply != "respuesta final" {
		t.Fatalf("prompt/reply = %q/%q", transport.prompt, result.Reply)
	}
}

func TestVoiceResponderStreamsOnlyTransportDeltas(t *testing.T) {
	transport := &promptClientStub{
		result: Result{Reply: "Hola mundo.", StopReason: "end_turn"},
		deltas: []string{"Hola", " mundo."},
	}
	responder, err := NewVoiceResponder(transport)
	if err != nil {
		t.Fatalf("NewVoiceResponder() error = %v", err)
	}
	var got []string
	result, err := responder.RespondStream(context.Background(), "saluda", llm.ContentDelta(func(delta string) error {
		got = append(got, delta)
		return nil
	}))
	if err != nil {
		t.Fatalf("RespondStream() error = %v", err)
	}
	if !reflect.DeepEqual(got, transport.deltas) {
		t.Fatalf("deltas = %q, want %q", got, transport.deltas)
	}
	if result.Reply != "Hola mundo." {
		t.Fatalf("Reply = %q", result.Reply)
	}
}

func TestVoiceResponderPropagatesCancellation(t *testing.T) {
	transport := &promptClientStub{err: context.Canceled}
	responder, err := NewVoiceResponder(transport)
	if err != nil {
		t.Fatalf("NewVoiceResponder() error = %v", err)
	}
	if _, err := responder.Respond(context.Background(), "hola"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Respond() error = %v, want context.Canceled", err)
	}
}

func TestVoiceResponderRejectsNilTransport(t *testing.T) {
	if _, err := NewVoiceResponder(nil); err == nil {
		t.Fatal("NewVoiceResponder(nil) error = nil")
	}
}
