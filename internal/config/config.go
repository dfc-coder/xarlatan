package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration structure.
type Config struct {
	Audio AudioConfig `yaml:"audio"`
	STT   STTConfig   `yaml:"stt"`
	LLM   LLMConfig   `yaml:"llm"`
	TTS   TTSConfig   `yaml:"tts"`
	Tools ToolsConfig `yaml:"tools"`
	Log   LogConfig   `yaml:"log"`
}

// ToolsConfig enables/configures tool use.
type ToolsConfig struct {
	// FSRoot sandboxes filesystem tools to this directory.
	// Empty string means unrestricted (use with care).
	FSRoot    string          `yaml:"fs_root"`
	WebSearch WebSearchConfig `yaml:"web_search"`
}

// WebSearchConfig selects the search backend (provider: duckduckgo|brave|searxng).
type WebSearchConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"` // SearXNG instance URL
}

type AudioConfig struct {
	SampleRate        int     `yaml:"sample_rate"`
	Channels          int     `yaml:"channels"`
	SilenceThreshold  float64 `yaml:"silence_threshold"`
	SilenceDurationMS int     `yaml:"silence_duration_ms"`
	MaxDurationS      int     `yaml:"max_duration_s"`
	Device            string  `yaml:"device"`
}

// SilenceDuration converts SilenceDurationMS to time.Duration.
func (a AudioConfig) SilenceDuration() time.Duration {
	return time.Duration(a.SilenceDurationMS) * time.Millisecond
}

// MaxDuration converts MaxDurationS to time.Duration.
func (a AudioConfig) MaxDuration() time.Duration {
	return time.Duration(a.MaxDurationS) * time.Second
}

type STTConfig struct {
	Encoder   string `yaml:"encoder"`
	Decoder   string `yaml:"decoder"`
	Tokens    string `yaml:"tokens"`
	Language  string `yaml:"language"`
	Translate bool   `yaml:"translate"`
}

type LLMConfig struct {
	ServerBinary string  `yaml:"server_binary"`
	Model        string  `yaml:"model"`
	Host         string  `yaml:"host"`
	Port         int     `yaml:"port"`
	ContextSize  int     `yaml:"context_size"`
	NGPULayers   int     `yaml:"n_gpu_layers"`
	Threads      int     `yaml:"threads"`
	Temperature  float64 `yaml:"temperature"`
	TopP         float64 `yaml:"top_p"`
	MaxTokens    int     `yaml:"max_tokens"`
	SystemPrompt string  `yaml:"system_prompt"`
}

// BaseURL returns the llama-server base URL.
func (l LLMConfig) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", l.Host, l.Port)
}

type TTSConfig struct {
	Model       string  `yaml:"model"`
	Tokens      string  `yaml:"tokens"`
	DataDir     string  `yaml:"data_dir"`
	SpeakerID   int     `yaml:"speaker_id"`
	LengthScale float64 `yaml:"length_scale"`
	NoiseScale  float64 `yaml:"noise_scale"`
	NoiseW      float64 `yaml:"noise_w"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

// Load reads the YAML config file at path (defaults to "config.yaml").
// Environment variable ASSISTANT_CONFIG overrides the path.
func Load(path string) (*Config, error) {
	if env := os.Getenv("ASSISTANT_CONFIG"); env != "" {
		path = env
	}
	if path == "" {
		path = "config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.setDefaults()
	return cfg, nil
}

func (c *Config) setDefaults() {
	if c.Tools.WebSearch.Provider == "" {
		c.Tools.WebSearch.Provider = "duckduckgo"
	}
	if c.STT.Language == "" {
		c.STT.Language = "auto"
	}
	if c.Audio.SampleRate == 0 {
		c.Audio.SampleRate = 16000
	}
	if c.Audio.Channels == 0 {
		c.Audio.Channels = 1
	}
	if c.Audio.SilenceThreshold == 0 {
		c.Audio.SilenceThreshold = 0.015
	}
	if c.Audio.SilenceDurationMS == 0 {
		c.Audio.SilenceDurationMS = 1500
	}
	if c.Audio.MaxDurationS == 0 {
		c.Audio.MaxDurationS = 30
	}
	if c.Audio.Device == "" {
		c.Audio.Device = "default"
	}
	if c.LLM.Host == "" {
		c.LLM.Host = "127.0.0.1"
	}
	if c.LLM.Port == 0 {
		c.LLM.Port = 8080
	}
	if c.LLM.ContextSize == 0 {
		c.LLM.ContextSize = 4096
	}
	if c.LLM.Threads == 0 {
		c.LLM.Threads = 4
	}
	if c.LLM.Temperature == 0 {
		c.LLM.Temperature = 0.7
	}
	if c.LLM.TopP == 0 {
		c.LLM.TopP = 0.9
	}
	if c.LLM.MaxTokens == 0 {
		c.LLM.MaxTokens = 512
	}
	if c.TTS.LengthScale == 0 {
		c.TTS.LengthScale = 1.0
	}
	if c.TTS.NoiseScale == 0 {
		c.TTS.NoiseScale = 0.667
	}
	if c.TTS.NoiseW == 0 {
		c.TTS.NoiseW = 0.8
	}
}
