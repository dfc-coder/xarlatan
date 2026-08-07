package application

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestCoordinatorStreamsPhrasesToAudioBeforeResponderCompletes(t *testing.T) {
	firstWrite := make(chan struct{})
	responder := &coordinatorStreamingResponder{
		deltas:     []string{"Primera. ", "Segunda."},
		finalReply: "Primera. Segunda.",
		waitAfterFirstDelta: firstWrite,
	}
	synthesizer := &recordingStreamingSynthesizer{}
	player := newRecordingStreamingPlayer(firstWrite)
	observer := &recordingObserver{}
	view := &recordingView{}
	coordinator := newStreamingCoordinator(t, responder, synthesizer, player, observer, view)

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Reply != "Primera. Segunda." {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if got := synthesizer.snapshot(); !reflect.DeepEqual(got, []string{"Primera.", "Segunda."}) {
		t.Fatalf("synthesized phrases = %v", got)
	}
	writes, finishes, plays, stops := player.snapshot()
	if writes != 2 || finishes != 1 || plays != 0 || stops != 0 {
		t.Fatalf("player writes=%d finishes=%d plays=%d stops=%d", writes, finishes, plays, stops)
	}
	if !responder.observedAudioBeforeReturn() {
		t.Fatal("responder completed before first playback write")
	}
	wantStates := []State{StateIdle, StateListening, StateTranscribing, StateThinking, StateSynthesizing, StateSpeaking, StateIdle}
	if got := observer.states(); !reflect.DeepEqual(got, wantStates) {
		t.Fatalf("states = %v, want %v", got, wantStates)
	}
	if result.Trace.FirstResponseDelta <= 0 || result.Trace.FirstAudio <= 0 {
		t.Fatalf("stream latency metadata missing: %+v", result.Trace)
	}
	if result.Trace.FirstAudio < result.Trace.FirstResponseDelta {
		t.Fatalf("first audio %v precedes first delta %v", result.Trace.FirstAudio, result.Trace.FirstResponseDelta)
	}
	if !reflect.DeepEqual(view.assistant, []string{"Primera. Segunda."}) {
		t.Fatalf("assistant view = %v", view.assistant)
	}
}

func TestCoordinatorFallsBackToBufferedPlaybackWhenStreamingProducesNoDeltas(t *testing.T) {
	responder := &coordinatorStreamingResponder{finalReply: "respuesta con tools"}
	synthesizer := &recordingStreamingSynthesizer{}
	player := newRecordingStreamingPlayer(nil)
	coordinator := newStreamingCoordinator(t, responder, synthesizer, player, nil, nil)

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if result.Reply != "respuesta con tools" {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if got := synthesizer.snapshot(); !reflect.DeepEqual(got, []string{"respuesta con tools"}) {
		t.Fatalf("synthesized = %v", got)
	}
	writes, finishes, plays, stops := player.snapshot()
	if writes != 0 || finishes != 0 || plays != 1 || stops != 0 {
		t.Fatalf("fallback player writes=%d finishes=%d plays=%d stops=%d", writes, finishes, plays, stops)
	}
	if result.Trace.FirstResponseDelta != 0 {
		t.Fatalf("FirstResponseDelta = %v, want 0 for buffered fallback", result.Trace.FirstResponseDelta)
	}
}

func newStreamingCoordinator(
	t *testing.T,
	responder Responder,
	synthesizer Synthesizer,
	player Player,
	observer Observer,
	view View,
) *Coordinator {
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
		Observer:    observer,
		View:        view,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator
}

type coordinatorStreamingResponder struct {
	mu                  sync.Mutex
	deltas              []string
	finalReply          string
	waitAfterFirstDelta <-chan struct{}
	audioBeforeReturn   bool
}

func (r *coordinatorStreamingResponder) Respond(context.Context, string) (conversation.Result, error) {
	return conversation.Result{Reply: r.finalReply}, nil
}

func (r *coordinatorStreamingResponder) RespondStream(ctx context.Context, _ string, onDelta llm.ContentDelta) (conversation.Result, error) {
	for index, delta := range r.deltas {
		if err := onDelta(delta); err != nil {
			return conversation.Result{}, err
		}
		if index == 0 && r.waitAfterFirstDelta != nil {
			select {
			case <-ctx.Done():
				return conversation.Result{}, ctx.Err()
			case <-r.waitAfterFirstDelta:
				r.mu.Lock()
				r.audioBeforeReturn = true
				r.mu.Unlock()
			}
		}
	}
	return conversation.Result{Reply: r.finalReply}, nil
}

func (r *coordinatorStreamingResponder) observedAudioBeforeReturn() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.audioBeforeReturn
}

type recordingStreamingSynthesizer struct {
	mu    sync.Mutex
	texts []string
}

func (s *recordingStreamingSynthesizer) Synthesize(_ context.Context, text string) (audio.Buffer, error) {
	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()
	return audio.Buffer{Samples: []float32{0.25}, SampleRate: 22050, Channels: 1}, nil
}

func (s *recordingStreamingSynthesizer) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

type recordingStreamingPlayer struct {
	mu          sync.Mutex
	writes      int
	finishes    int
	plays       int
	stops       int
	firstWrite  chan struct{}
	writeOnce   sync.Once
}

func newRecordingStreamingPlayer(firstWrite chan struct{}) *recordingStreamingPlayer {
	return &recordingStreamingPlayer{firstWrite: firstWrite}
}

func (p *recordingStreamingPlayer) Play(context.Context, audio.Buffer) error {
	p.mu.Lock()
	p.plays++
	p.mu.Unlock()
	return nil
}

func (p *recordingStreamingPlayer) Write(context.Context, audio.Buffer) error {
	p.mu.Lock()
	p.writes++
	p.mu.Unlock()
	if p.firstWrite != nil {
		p.writeOnce.Do(func() { close(p.firstWrite) })
	}
	return nil
}

func (p *recordingStreamingPlayer) Finish(context.Context) error {
	p.mu.Lock()
	p.finishes++
	p.mu.Unlock()
	return nil
}

func (p *recordingStreamingPlayer) Stop() error {
	p.mu.Lock()
	p.stops++
	p.mu.Unlock()
	return nil
}

func (p *recordingStreamingPlayer) snapshot() (writes, finishes, plays, stops int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writes, p.finishes, p.plays, p.stops
}

var _ StreamingResponder = (*coordinatorStreamingResponder)(nil)
var _ StreamingPlayer = (*recordingStreamingPlayer)(nil)
