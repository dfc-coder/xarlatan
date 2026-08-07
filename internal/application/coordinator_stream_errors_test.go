package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestCoordinatorStreamingClassifiesSynthesisFailure(t *testing.T) {
	coordinator := newStreamingFailureCoordinator(
		t,
		&fixedStreamingResponder{deltas: []string{"Primera."}, finalReply: "Primera."},
		synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			return audio.Buffer{}, errors.New("tts failed")
		}),
		&failingStreamingPlayer{},
	)

	_, err := coordinator.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorSynthesisFailed) {
		t.Fatalf("RunTurn() error = %v, want synthesis_failed", err)
	}
}

func TestCoordinatorStreamingClassifiesPlaybackWriteFailure(t *testing.T) {
	player := &failingStreamingPlayer{writeErr: errors.New("speaker write failed")}
	coordinator := newStreamingFailureCoordinator(
		t,
		&fixedStreamingResponder{deltas: []string{"Primera."}, finalReply: "Primera."},
		streamingTestSynthesizer(),
		player,
	)

	_, err := coordinator.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorPlaybackFailed) {
		t.Fatalf("RunTurn() error = %v, want playback_failed", err)
	}
	if player.writeCalls != 1 {
		t.Fatalf("Write calls = %d, want 1", player.writeCalls)
	}
}

func TestCoordinatorStreamingClassifiesPlaybackFinishFailure(t *testing.T) {
	player := &failingStreamingPlayer{finishErr: errors.New("drain failed")}
	coordinator := newStreamingFailureCoordinator(
		t,
		&fixedStreamingResponder{deltas: []string{"Primera."}, finalReply: "Primera."},
		streamingTestSynthesizer(),
		player,
	)

	_, err := coordinator.RunTurn(context.Background())
	if !IsErrorCode(err, ErrorPlaybackFailed) {
		t.Fatalf("RunTurn() error = %v, want playback_failed", err)
	}
	if player.writeCalls != 1 || player.finishCalls != 1 {
		t.Fatalf("player writes=%d finishes=%d", player.writeCalls, player.finishCalls)
	}
}

func TestCoordinatorStreamingCancellationStopsPartialPlayback(t *testing.T) {
	firstWrite := make(chan struct{})
	player := &failingStreamingPlayer{firstWrite: firstWrite}
	responder := &blockingStreamingResponder{firstWrite: firstWrite}
	coordinator := newStreamingFailureCoordinator(t, responder, streamingTestSynthesizer(), player)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := coordinator.RunTurn(ctx)
		done <- err
	}()
	<-firstWrite
	cancel()

	if err := <-done; !IsErrorCode(err, ErrorCancelled) {
		t.Fatalf("RunTurn() error = %v, want cancelled", err)
	}
	if player.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want 1", player.stopCalls)
	}
}

func TestTurnRecorderCopiesStreamingLatencyMetadata(t *testing.T) {
	recorder := newTurnRecorder(7, nopObserver{})
	recorder.markFirstResponseDelta()
	recorder.emit(StateSpeaking)
	trace := recorder.finish(Trace{})
	if trace.FirstResponseDelta <= 0 || trace.FirstAudio <= 0 {
		t.Fatalf("latency metadata = %+v", trace)
	}
}

func newStreamingFailureCoordinator(t *testing.T, responder Responder, synthesizer Synthesizer, player Player) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			return audio.Buffer{Samples: []float32{0.1, 0.2, 0.3}, SampleRate: 16000, Channels: 1}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "hola", nil
		}),
		Responder:   responder,
		Synthesizer: synthesizer,
		Player:      player,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator
}

func streamingTestSynthesizer() Synthesizer {
	return synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
		return audio.Buffer{Samples: []float32{0.2}, SampleRate: 22050, Channels: 1}, nil
	})
}

type fixedStreamingResponder struct {
	deltas     []string
	finalReply string
}

func (r *fixedStreamingResponder) Respond(context.Context, string) (conversation.Result, error) {
	return conversation.Result{Reply: r.finalReply}, nil
}

func (r *fixedStreamingResponder) RespondStream(_ context.Context, _ string, onDelta llm.ContentDelta) (conversation.Result, error) {
	for _, delta := range r.deltas {
		if err := onDelta(delta); err != nil {
			return conversation.Result{}, err
		}
	}
	return conversation.Result{Reply: r.finalReply}, nil
}

type blockingStreamingResponder struct {
	firstWrite <-chan struct{}
}

func (r *blockingStreamingResponder) Respond(context.Context, string) (conversation.Result, error) {
	return conversation.Result{Reply: "Primera."}, nil
}

func (r *blockingStreamingResponder) RespondStream(ctx context.Context, _ string, onDelta llm.ContentDelta) (conversation.Result, error) {
	if err := onDelta("Primera."); err != nil {
		return conversation.Result{}, err
	}
	select {
	case <-ctx.Done():
		return conversation.Result{}, ctx.Err()
	case <-r.firstWrite:
	}
	<-ctx.Done()
	return conversation.Result{}, ctx.Err()
}

type failingStreamingPlayer struct {
	writeErr    error
	finishErr   error
	writeCalls  int
	finishCalls int
	stopCalls   int
	playCalls   int
	firstWrite  chan struct{}
	writeClosed bool
}

func (p *failingStreamingPlayer) Play(context.Context, audio.Buffer) error {
	p.playCalls++
	return nil
}

func (p *failingStreamingPlayer) Write(context.Context, audio.Buffer) error {
	p.writeCalls++
	if p.firstWrite != nil && !p.writeClosed {
		close(p.firstWrite)
		p.writeClosed = true
	}
	return p.writeErr
}

func (p *failingStreamingPlayer) Finish(context.Context) error {
	p.finishCalls++
	return p.finishErr
}

func (p *failingStreamingPlayer) Stop() error {
	p.stopCalls++
	return nil
}

var _ StreamingResponder = (*fixedStreamingResponder)(nil)
var _ StreamingResponder = (*blockingStreamingResponder)(nil)
var _ StreamingPlayer = (*failingStreamingPlayer)(nil)
