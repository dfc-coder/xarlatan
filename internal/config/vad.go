package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultSileroVADFilename = "silero_vad.onnx"

// VADRuntimeConfig is derived from the validated audio/model layout rather than
// introducing a second set of overlapping audio-timing knobs. Custom model
// layouts can override only the model path with XARLATAN_VAD_MODEL.
type VADRuntimeConfig struct {
	Model              string
	Threshold          float32
	MinSilenceDuration time.Duration
	MinSpeechDuration  time.Duration
	MaxSpeechDuration  time.Duration
	SampleRate         int
	NumThreads         int
	Provider           string
	WindowSize         int
	BufferSize         time.Duration
}

// SileroVADConfig resolves the Phase 2 VAD runtime from the existing model
// layout. Standard layouts are:
//
//	models/stt/<model>/...            -> models/vad/silero_vad.onnx
//	/var/lib/xarlatan/models/stt/...  -> /var/lib/xarlatan/models/vad/silero_vad.onnx
func (c Config) SileroVADConfig() (VADRuntimeConfig, error) {
	if c.Audio.SampleRate != 16000 {
		return VADRuntimeConfig{}, fmt.Errorf("silero VAD requires audio.sample_rate=16000")
	}
	model := strings.TrimSpace(os.Getenv("XARLATAN_VAD_MODEL"))
	if model == "" {
		model = inferVADModelPath(c.STT.Encoder)
	}
	if err := validateRegularFile("vad.model", model); err != nil {
		return VADRuntimeConfig{}, err
	}
	maxSpeech := c.Audio.MaxDuration()
	return VADRuntimeConfig{
		Model:              model,
		Threshold:          0.5,
		MinSilenceDuration: c.Audio.SilenceDuration(),
		MinSpeechDuration:  250 * time.Millisecond,
		MaxSpeechDuration:  maxSpeech,
		SampleRate:         c.Audio.SampleRate,
		NumThreads:         1,
		Provider:           "cpu",
		WindowSize:         512,
		BufferSize:         maxSpeech + 5*time.Second,
	}, nil
}

func inferVADModelPath(sttEncoder string) string {
	encoderDir := filepath.Dir(filepath.Clean(sttEncoder))
	sttDir := filepath.Dir(encoderDir)
	modelsDir := filepath.Dir(sttDir)
	return filepath.Join(modelsDir, "vad", defaultSileroVADFilename)
}
