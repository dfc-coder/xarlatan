package application

import (
	"context"
	"reflect"
	"testing"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestCoordinatorStreamingDiscardsStaleTurnDelta(t *testing.T) {
	responder := &fixedStreamingResponder{deltas: []string{"Actual."}, finalReply: "Actual."}
	synthesizer := &recordingStreamingSynthesizer{}
	player := newRecordingStreamingPlayer(nil)
	dependencies := Dependencies{
		Input: voiceInputFunc(func(context.Context) (audio.Buffer, error) {
			return audio.Buffer{}, nil
		}),
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			return "", nil
		}),
		Responder:   responder,
		Synthesizer: synthesizer,
		Player:      player,
		Observer:    nopObserver{},
		View:        nopView{},
	}
	coordinator := &Coordinator{dependencies: dependencies}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workers := newWorkerSet(ctx, dependencies)
	workers.start()
	defer workers.stop()

	const currentTurn uint64 = 42
	workers.deltas <- responseDelta{turnID: currentTurn - 1, text: "Obsoleto."}
	recorder := newTurnRecorder(currentTurn, nopObserver{})
	recorder.emit(StateThinking)

	result, err := coordinator.completeStreamingResponse(
		ctx,
		currentTurn,
		"hola",
		workers,
		recorder,
		Result{},
	)
	if err != nil {
		t.Fatalf("completeStreamingResponse() error = %v", err)
	}
	if result.Reply != "Actual." {
		t.Fatalf("Reply = %q", result.Reply)
	}
	if got := synthesizer.snapshot(); !reflect.DeepEqual(got, []string{"Actual."}) {
		t.Fatalf("synthesized phrases = %v; stale delta reached TTS", got)
	}
	writes, finishes, plays, stops := player.snapshot()
	if writes != 1 || finishes != 1 || plays != 0 || stops != 0 {
		t.Fatalf("player writes=%d finishes=%d plays=%d stops=%d", writes, finishes, plays, stops)
	}
}
