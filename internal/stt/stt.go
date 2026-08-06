// Package stt provides speech-to-text via sherpa-onnx.
package stt

import (
	"context"
	"fmt"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/dfc-coder/xarlatan/internal/audio"
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
	if t != nil && t.recognizer != nil {
		sherpa.DeleteOfflineRecognizer(t.recognizer)
		t.recognizer = nil
	}
	return nil
}

// Transcribe converts one normalized mono buffer to text. The offline CGo
// decode cannot be preempted after it starts, so cancellation is checked before
// and immediately after the decode boundary.
func (t *Transcriber) Transcribe(ctx context.Context, buffer audio.Buffer) (string, error) {
	if t == nil || t.recognizer == nil {
		return "", fmt.Errorf("transcriber is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if buffer.Empty() {
		return "", nil
	}
	if err := buffer.Validate(); err != nil {
		return "", err
	}
	if buffer.SampleRate != t.sampleRate {
		return "", fmt.Errorf("unexpected sample rate %d, want %d", buffer.SampleRate, t.sampleRate)
	}

	stream := sherpa.NewOfflineStream(t.recognizer)
	if stream == nil {
		return "", fmt.Errorf("sherpa stream: nil")
	}
	defer sherpa.DeleteOfflineStream(stream)

	stream.AcceptWaveform(buffer.SampleRate, buffer.Samples)
	t.recognizer.Decode(stream)
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return filterTranscript(stream.GetResult().Text), nil
}
