// Package stt provides speech-to-text via sherpa-onnx.
package stt

import (
	"fmt"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/dfc-coder/xarlatan/internal/config"
)

// Transcriber wraps a sherpa-onnx Whisper recognizer.
type Transcriber struct {
	recognizer *sherpa.OfflineRecognizer
	sampleRate int
}

// New loads a sherpa-onnx Whisper model set.
func New(cfg config.STTConfig, sampleRate int) (*Transcriber, error) {
	recognizerCfg := sherpa.OfflineRecognizerConfig{}
	recognizerCfg.FeatConfig.SampleRate = sampleRate
	recognizerCfg.FeatConfig.FeatureDim = 80
	recognizerCfg.ModelConfig.Whisper.Encoder = cfg.Encoder
	recognizerCfg.ModelConfig.Whisper.Decoder = cfg.Decoder
	recognizerCfg.ModelConfig.Tokens = cfg.Tokens
	recognizerCfg.ModelConfig.Whisper.TailPaddings = -1
	if cfg.Language != "" && cfg.Language != "auto" {
		recognizerCfg.ModelConfig.Whisper.Language = cfg.Language
	}
	if cfg.Translate {
		recognizerCfg.ModelConfig.Whisper.Task = "translate"
	} else {
		recognizerCfg.ModelConfig.Whisper.Task = "transcribe"
	}
	recognizerCfg.ModelConfig.NumThreads = 4
	recognizerCfg.ModelConfig.Provider = "cpu"
	recognizerCfg.ModelConfig.Debug = 0

	recognizer := sherpa.NewOfflineRecognizer(&recognizerCfg)
	if recognizer == nil {
		return nil, fmt.Errorf("sherpa recognizer: nil")
	}
	return &Transcriber{recognizer: recognizer, sampleRate: sampleRate}, nil
}

// Close releases model resources.
func (t *Transcriber) Close() error {
	if t.recognizer != nil {
		sherpa.DeleteOfflineRecognizer(t.recognizer)
		t.recognizer = nil
	}
	return nil
}

// Transcribe converts float32 PCM samples (16 kHz, mono) to text.
func (t *Transcriber) Transcribe(samples []float32) (string, error) {
	stream := sherpa.NewOfflineStream(t.recognizer)
	defer sherpa.DeleteOfflineStream(stream)

	stream.AcceptWaveform(t.sampleRate, samples)
	t.recognizer.Decode(stream)

	text := strings.TrimSpace(stream.GetResult().Text)
	return text, nil
}
