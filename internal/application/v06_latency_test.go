package application

import (
	"context"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
	"github.com/dfc-coder/xarlatan/internal/conversation"
	"github.com/dfc-coder/xarlatan/internal/llm"
)

func TestCoordinatorRecordsV06LatencyBoundaries(t *testing.T) {
	input := &timedVoiceInput{}
	responder := &coordinatorStreamingResponder{
		deltas:     []string{"Primera frase."},
		finalReply: "Primera frase.",
	}
	player := newRecordingStreamingPlayer(nil)
	coordinator, err := NewCoordinator(Dependencies{
		Input: input,
		Transcriber: transcriberFunc(func(context.Context, audio.Buffer) (string, error) {
			time.Sleep(time.Millisecond)
			return "xarlatan prueba", nil
		}),
		Responder: responder,
		Synthesizer: synthesizerFunc(func(context.Context, string) (audio.Buffer, error) {
			time.Sleep(time.Millisecond)
			return audio.Buffer{Samples: []float32{0.1}, SampleRate: 24000, Channels: 1}, nil
		}),
		Player: player,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	result, err := coordinator.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn() error = %v", err)
	}
	trace := result.Trace
	for name, value := range map[string]time.Duration{
		"end_of_speech":        trace.EndOfSpeech,
		"final_transcript":     trace.FinalTranscript,
		"first_response_delta": trace.FirstResponseDelta,
		"first_pcm":            trace.FirstPCM,
		"first_audio":          trace.FirstAudio,
		"stt_latency":          trace.STTLatency,
		"agent_ttft":           trace.AgentTTFT,
		"tts_first_chunk":      trace.TTSFirstChunkLatency,
		"playback_latency":     trace.PlaybackLatency,
		"eos_to_first_audio":   trace.EOSToFirstAudio,
	} {
		if value <= 0 {
			t.Fatalf("%s = %v, want positive metadata; trace=%+v", name, value, trace)
		}
	}
	if !(trace.EndOfSpeech <= trace.FinalTranscript && trace.FinalTranscript <= trace.FirstResponseDelta && trace.FirstResponseDelta <= trace.FirstPCM && trace.FirstPCM <= trace.FirstAudio) {
		t.Fatalf("latency boundaries out of order: %+v", trace)
	}
}

type timedVoiceInput struct {
	endedAt time.Time
}

func (i *timedVoiceInput) Next(context.Context) (audio.Buffer, error) {
	i.endedAt = time.Now()
	return audio.Buffer{Samples: []float32{0.1, 0.2, 0.3}, SampleRate: 16000, Channels: 1}, nil
}

func (i *timedVoiceInput) ConsumeEndOfSpeech() time.Time {
	value := i.endedAt
	i.endedAt = time.Time{}
	return value
}

var _ VoiceInput = (*timedVoiceInput)(nil)
var _ StreamingResponder = (*coordinatorStreamingResponder)(nil)
var _ llm.ContentDelta = llm.ContentDelta(nil)
var _ conversation.Result = conversation.Result{}
