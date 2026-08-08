package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// VoiceConfig owns the voice-gateway inference plane. It is independent from
// the legacy sherpa/VITS configuration kept temporarily for rollback.
type VoiceConfig struct {
	Worker VoiceWorkerConfig    `yaml:"worker"`
	VAD    VoiceVADConfig       `yaml:"vad"`
	STT    VoiceInferenceConfig `yaml:"stt"`
	TTS    VoiceInferenceConfig `yaml:"tts"`
}

type VoiceWorkerConfig struct {
	Python string `yaml:"python"`
	Script string `yaml:"script"`
}

type VoiceVADConfig struct {
	Model string `yaml:"model"`
}

type VoiceInferenceConfig struct {
	Python         string `yaml:"python"`
	ModelDir       string `yaml:"model_dir"`
	VoiceFile      string `yaml:"voice_file"`
	Language       string `yaml:"language"`
	Device         string `yaml:"device"`
	FallbackDevice string `yaml:"fallback_device"`
	CacheDir       string `yaml:"cache_dir"`
}

func (c *Config) applyVoiceDefaults() {
	if c.Voice.STT.Language == "" {
		c.Voice.STT.Language = "es"
	}
	if c.Voice.STT.Device == "" {
		c.Voice.STT.Device = "GPU"
	}
	if c.Voice.STT.FallbackDevice == "" {
		c.Voice.STT.FallbackDevice = "CPU"
	}
	if c.Voice.TTS.Language == "" {
		c.Voice.TTS.Language = "es"
	}
	if c.Voice.TTS.Device == "" {
		c.Voice.TTS.Device = "GPU"
	}
	if c.Voice.TTS.FallbackDevice == "" {
		c.Voice.TTS.FallbackDevice = "CPU"
	}
}

func validateVoiceGateway(cfg VoiceConfig) error {
	if err := validateRegularFile("voice.worker.script", cfg.Worker.Script); err != nil {
		return err
	}
	if err := validateRegularFile("voice.vad.model", cfg.VAD.Model); err != nil {
		return err
	}
	if err := validateVoiceInference("voice.stt", cfg.Worker.Python, cfg.STT, false); err != nil {
		return err
	}
	if err := validateVoiceInference("voice.tts", cfg.Worker.Python, cfg.TTS, true); err != nil {
		return err
	}
	return nil
}

func validateVoiceInference(name, defaultPython string, cfg VoiceInferenceConfig, requireVoice bool) error {
	python := strings.TrimSpace(cfg.Python)
	if python == "" {
		python = strings.TrimSpace(defaultPython)
	}
	if err := validateExecutable(name+".python", python); err != nil {
		return err
	}
	if err := validateDirectory(name+".model_dir", cfg.ModelDir); err != nil {
		return err
	}
	if requireVoice {
		if err := validateRegularFile(name+".voice_file", cfg.VoiceFile); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.Language) == "" {
		return fmt.Errorf("%s.language is required", name)
	}
	if strings.TrimSpace(cfg.Device) == "" {
		return fmt.Errorf("%s.device is required", name)
	}
	if strings.TrimSpace(cfg.FallbackDevice) == "" {
		return fmt.Errorf("%s.fallback_device is required", name)
	}
	if cache := strings.TrimSpace(cfg.CacheDir); cache != "" && !filepath.IsAbs(cache) {
		return fmt.Errorf("%s.cache_dir must be absolute when set", name)
	}
	return nil
}
