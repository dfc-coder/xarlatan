package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateVoiceGatewayUsesVoiceModelsNotLegacySTTOrTTS(t *testing.T) {
	cfg := operationalConfig(t)
	configureVoiceGatewayFixture(t, &cfg)
	cfg.STT = STTConfig{}
	cfg.TTS = TTSConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v; voice_gateway must not require legacy STT/TTS", err)
	}
}

func TestValidateVoiceGatewayRequiresKokoroVoiceEmbedding(t *testing.T) {
	cfg := operationalConfig(t)
	configureVoiceGatewayFixture(t, &cfg)
	cfg.Voice.TTS.VoiceFile = filepath.Join(t.TempDir(), "missing-ef_dora.bin")
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "voice.tts.voice_file") {
		t.Fatalf("Validate() error = %v, want voice.tts.voice_file", err)
	}
}

func TestVoiceDefaultsPreferIntelGPUForSTTAndSpanishKokoro(t *testing.T) {
	path := writeTestConfig(t, "audio: {}")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Voice.STT.Device != "GPU" || cfg.Voice.STT.FallbackDevice != "CPU" || cfg.Voice.STT.Language != "es" {
		t.Fatalf("voice STT defaults = %+v", cfg.Voice.STT)
	}
	if cfg.Voice.TTS.Device != "GPU" || cfg.Voice.TTS.FallbackDevice != "CPU" || cfg.Voice.TTS.Language != "es" {
		t.Fatalf("voice TTS defaults = %+v", cfg.Voice.TTS)
	}
}

func TestSileroVADConfigUsesExplicitVoiceModel(t *testing.T) {
	cfg := operationalConfig(t)
	configureVoiceGatewayFixture(t, &cfg)
	got, err := cfg.SileroVADConfig()
	if err != nil {
		t.Fatalf("SileroVADConfig() error = %v", err)
	}
	if got.Model != cfg.Voice.VAD.Model {
		t.Fatalf("VAD model = %q, want %q", got.Model, cfg.Voice.VAD.Model)
	}
	if got.Provider != "cpu" {
		t.Fatalf("VAD provider = %q, want cpu", got.Provider)
	}
}

func configureVoiceGatewayFixture(t *testing.T, cfg *Config) {
	t.Helper()
	root := t.TempDir()
	zeroClaw := filepath.Join(root, "zeroclaw")
	python := filepath.Join(root, "python3")
	workerScript := filepath.Join(root, "openvino_voice_worker.py")
	vadModel := filepath.Join(root, "silero_vad.onnx")
	voiceFile := filepath.Join(root, "ef_dora.bin")
	for _, path := range []string{zeroClaw, python} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatalf("write executable: %v", err)
		}
	}
	for _, path := range []string{workerScript, vadModel, voiceFile} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	sttDir := filepath.Join(root, "whisper")
	ttsDir := filepath.Join(root, "kokoro")
	for _, path := range []string{sttDir, ttsDir} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	cfg.Agent = AgentConfig{Mode: "voice_gateway", ZeroClaw: ZeroClawConfig{Binary: zeroClaw, AgentAlias: "xarlatan"}}
	cfg.Voice = VoiceConfig{
		Worker: VoiceWorkerConfig{Python: python, Script: workerScript},
		VAD:    VoiceVADConfig{Model: vadModel},
		STT: VoiceInferenceConfig{
			ModelDir: sttDir, Device: "GPU", FallbackDevice: "CPU", Language: "es",
		},
		TTS: VoiceInferenceConfig{
			ModelDir: ttsDir, VoiceFile: voiceFile, Device: "GPU", FallbackDevice: "CPU", Language: "es",
		},
	}
}
