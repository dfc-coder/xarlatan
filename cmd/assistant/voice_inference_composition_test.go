package main

import (
	"strings"
	"testing"
)

func TestVoiceGatewayInferenceUsesPersistentOpenVINOWorkers(t *testing.T) {
	source := readCompositionSource(t, "voice_gateway_inference.go")
	for _, required := range []string{
		"inference.StartWorker(",
		`Mode:            "stt"`,
		`Mode:            "tts"`,
		"cfg.Voice.TTS.VoiceFile",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("voice gateway inference missing %q", required)
		}
	}
	for _, forbidden := range []string{"stt.New(", "tts.New("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("voice gateway inference contains legacy primitive %q", forbidden)
		}
	}
}

func TestMainUsesVoiceInferenceRuntimeBoundary(t *testing.T) {
	source := readMainSource(t)
	if !strings.Contains(source, "newVoiceInferenceRuntime(ctx, cfg)") {
		t.Fatal("main.go must construct voice inference through newVoiceInferenceRuntime")
	}
	for _, forbidden := range []string{"stt.New(", "tts.New("} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("main.go contains concrete inference primitive %q", forbidden)
		}
	}
}
