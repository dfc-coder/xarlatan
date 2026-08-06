package config

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultFilesystemLimitBytes int64 = 1 << 20
	defaultLLMStartupTimeoutMS        = 60_000
	defaultLLMShutdownTimeoutMS       = 5_000
	defaultLLMHealthIntervalMS        = 250
)

// Config is the validated runtime configuration.
type Config struct {
	Audio AudioConfig `yaml:"audio"`
	STT   STTConfig   `yaml:"stt"`
	LLM   LLMConfig   `yaml:"llm"`
	TTS   TTSConfig   `yaml:"tts"`
	Tools ToolsConfig `yaml:"tools"`
	Log   LogConfig   `yaml:"log"`
}

// ToolsConfig enables and configures tool use.
type ToolsConfig struct {
	Filesystem FilesystemConfig `yaml:"filesystem"`
	WebSearch  WebSearchConfig  `yaml:"web_search"`
}

// FilesystemConfig controls filesystem tool exposure and hard payload limits.
// Mutations are disabled unless AllowMutations is explicitly true.
type FilesystemConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Root           string `yaml:"root"`
	AllowMutations bool   `yaml:"allow_mutations"`
	MaxReadBytes   int64  `yaml:"max_read_bytes"`
	MaxWriteBytes  int64  `yaml:"max_write_bytes"`
}

// WebSearchConfig selects the search backend (provider: duckduckgo|brave|searxng).
type WebSearchConfig struct {
	Provider string `yaml:"provider"`
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url"`
}

type AudioConfig struct {
	SampleRate        int     `yaml:"sample_rate"`
	Channels          int     `yaml:"channels"`
	SilenceThreshold  float64 `yaml:"silence_threshold"`
	SilenceDurationMS int     `yaml:"silence_duration_ms"`
	MaxDurationS      int     `yaml:"max_duration_s"`
	Device            string  `yaml:"device"`
}

func (a AudioConfig) SilenceDuration() time.Duration {
	return time.Duration(a.SilenceDurationMS) * time.Millisecond
}

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
	Mode              string  `yaml:"mode"`
	ServerBinary      string  `yaml:"server_binary"`
	Model             string  `yaml:"model"`
	Host              string  `yaml:"host"`
	Port              int     `yaml:"port"`
	ContextSize       int     `yaml:"context_size"`
	NGPULayers        int     `yaml:"n_gpu_layers"`
	Threads           int     `yaml:"threads"`
	Temperature       float64 `yaml:"temperature"`
	TopP              float64 `yaml:"top_p"`
	MaxTokens         int     `yaml:"max_tokens"`
	SystemPrompt      string  `yaml:"system_prompt"`
	StartupTimeoutMS  int     `yaml:"startup_timeout_ms"`
	ShutdownTimeoutMS int     `yaml:"shutdown_timeout_ms"`
	HealthIntervalMS  int     `yaml:"health_interval_ms"`
}

func (l LLMConfig) BaseURL() string {
	return fmt.Sprintf("http://%s:%d", l.Host, l.Port)
}

func (l LLMConfig) StartupTimeout() time.Duration {
	return time.Duration(l.StartupTimeoutMS) * time.Millisecond
}

func (l LLMConfig) ShutdownTimeout() time.Duration {
	return time.Duration(l.ShutdownTimeoutMS) * time.Millisecond
}

func (l LLMConfig) HealthInterval() time.Duration {
	return time.Duration(l.HealthIntervalMS) * time.Millisecond
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

// Load reads and strictly decodes YAML. It applies defaults but leaves
// environment-dependent checks to Validate.
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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("parsing trailing config document: %w", err)
		}
		return nil, fmt.Errorf("parsing config: multiple YAML documents are not supported")
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("reading config field presence: %w", err)
	}
	cfg.applyDefaults(&document)
	return cfg, nil
}

func (c *Config) applyDefaults(document *yaml.Node) {
	if c.Tools.WebSearch.Provider == "" {
		c.Tools.WebSearch.Provider = "duckduckgo"
	}
	if !hasYAMLPath(document, "tools", "filesystem", "max_read_bytes") {
		c.Tools.Filesystem.MaxReadBytes = defaultFilesystemLimitBytes
	}
	if !hasYAMLPath(document, "tools", "filesystem", "max_write_bytes") {
		c.Tools.Filesystem.MaxWriteBytes = defaultFilesystemLimitBytes
	}
	if c.STT.Language == "" {
		c.STT.Language = "auto"
	}
	if !hasYAMLPath(document, "audio", "sample_rate") {
		c.Audio.SampleRate = 16000
	}
	if !hasYAMLPath(document, "audio", "channels") {
		c.Audio.Channels = 1
	}
	if !hasYAMLPath(document, "audio", "silence_threshold") {
		c.Audio.SilenceThreshold = 0.015
	}
	if !hasYAMLPath(document, "audio", "silence_duration_ms") {
		c.Audio.SilenceDurationMS = 1500
	}
	if !hasYAMLPath(document, "audio", "max_duration_s") {
		c.Audio.MaxDurationS = 30
	}
	if c.Audio.Device == "" {
		c.Audio.Device = "default"
	}
	if c.LLM.Mode == "" {
		c.LLM.Mode = "managed"
	}
	if c.LLM.Host == "" {
		c.LLM.Host = "127.0.0.1"
	}
	if !hasYAMLPath(document, "llm", "port") {
		c.LLM.Port = 8080
	}
	if !hasYAMLPath(document, "llm", "context_size") {
		c.LLM.ContextSize = 4096
	}
	if !hasYAMLPath(document, "llm", "threads") {
		c.LLM.Threads = 4
	}
	if !hasYAMLPath(document, "llm", "temperature") {
		c.LLM.Temperature = 0.7
	}
	if !hasYAMLPath(document, "llm", "top_p") {
		c.LLM.TopP = 0.9
	}
	if !hasYAMLPath(document, "llm", "max_tokens") {
		c.LLM.MaxTokens = 512
	}
	if !hasYAMLPath(document, "llm", "startup_timeout_ms") {
		c.LLM.StartupTimeoutMS = defaultLLMStartupTimeoutMS
	}
	if !hasYAMLPath(document, "llm", "shutdown_timeout_ms") {
		c.LLM.ShutdownTimeoutMS = defaultLLMShutdownTimeoutMS
	}
	if !hasYAMLPath(document, "llm", "health_interval_ms") {
		c.LLM.HealthIntervalMS = defaultLLMHealthIntervalMS
	}
	if !hasYAMLPath(document, "tts", "length_scale") {
		c.TTS.LengthScale = 1
	}
	if !hasYAMLPath(document, "tts", "noise_scale") {
		c.TTS.NoiseScale = 0.667
	}
	if !hasYAMLPath(document, "tts", "noise_w") {
		c.TTS.NoiseW = 0.8
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}

func hasYAMLPath(document *yaml.Node, path ...string) bool {
	if document == nil || len(path) == 0 {
		return false
	}
	node := document
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return false
		}
		node = node.Content[0]
	}
	for _, segment := range path {
		if node.Kind != yaml.MappingNode {
			return false
		}
		found := false
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == segment {
				node = node.Content[i+1]
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Validate checks structural, security, and runtime prerequisites before any
// audio, model, process, or tool dependency is constructed.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.Audio.SampleRate <= 0 {
		return fmt.Errorf("audio.sample_rate must be greater than zero")
	}
	if c.Audio.Channels != 1 {
		return fmt.Errorf("audio.channels must be 1; multichannel capture is not supported")
	}
	if c.Audio.SilenceThreshold < 0 || c.Audio.SilenceThreshold > 1 {
		return fmt.Errorf("audio.silence_threshold must be between 0 and 1")
	}
	if c.Audio.SilenceDurationMS <= 0 {
		return fmt.Errorf("audio.silence_duration_ms must be greater than zero")
	}
	if c.Audio.MaxDurationS <= 0 {
		return fmt.Errorf("audio.max_duration_s must be greater than zero")
	}
	if strings.TrimSpace(c.Audio.Device) == "" {
		return fmt.Errorf("audio.device is required")
	}
	if err := validateLLM(c.LLM); err != nil {
		return err
	}
	if c.TTS.LengthScale <= 0 {
		return fmt.Errorf("tts.length_scale must be greater than zero")
	}
	if c.TTS.NoiseScale < 0 {
		return fmt.Errorf("tts.noise_scale must not be negative")
	}
	if c.TTS.NoiseW < 0 {
		return fmt.Errorf("tts.noise_w must not be negative")
	}
	if err := validateFilesystem(c.Tools.Filesystem); err != nil {
		return err
	}
	if err := validateWebSearch(c.Tools.WebSearch); err != nil {
		return err
	}
	if err := validateLogLevel(c.Log.Level); err != nil {
		return err
	}

	for _, item := range []struct {
		name string
		path string
	}{
		{"stt.encoder", c.STT.Encoder},
		{"stt.decoder", c.STT.Decoder},
		{"stt.tokens", c.STT.Tokens},
		{"tts.model", c.TTS.Model},
		{"tts.tokens", c.TTS.Tokens},
	} {
		if err := validateRegularFile(item.name, item.path); err != nil {
			return err
		}
	}
	if err := validateDirectory("tts.data_dir", c.TTS.DataDir); err != nil {
		return err
	}
	return nil
}

func validateLLM(cfg LLMConfig) error {
	if cfg.Mode != "managed" && cfg.Mode != "external" {
		return fmt.Errorf("llm.mode must be managed or external")
	}
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("llm.host is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("llm.port must be between 1 and 65535")
	}
	if cfg.ContextSize <= 0 {
		return fmt.Errorf("llm.context_size must be greater than zero")
	}
	if cfg.Threads <= 0 {
		return fmt.Errorf("llm.threads must be greater than zero")
	}
	if cfg.NGPULayers < 0 {
		return fmt.Errorf("llm.n_gpu_layers must not be negative")
	}
	if cfg.Temperature < 0 || cfg.Temperature > 2 {
		return fmt.Errorf("llm.temperature must be between 0 and 2")
	}
	if cfg.TopP <= 0 || cfg.TopP > 1 {
		return fmt.Errorf("llm.top_p must be greater than 0 and at most 1")
	}
	if cfg.MaxTokens <= 0 {
		return fmt.Errorf("llm.max_tokens must be greater than zero")
	}
	if cfg.StartupTimeoutMS <= 0 {
		return fmt.Errorf("llm.startup_timeout_ms must be greater than zero")
	}
	if cfg.ShutdownTimeoutMS <= 0 {
		return fmt.Errorf("llm.shutdown_timeout_ms must be greater than zero")
	}
	if cfg.HealthIntervalMS <= 0 {
		return fmt.Errorf("llm.health_interval_ms must be greater than zero")
	}
	if cfg.Mode == "external" {
		return nil
	}
	if err := validateRegularFile("llm.model", cfg.Model); err != nil {
		return err
	}
	return validateExecutable("llm.server_binary", cfg.ServerBinary)
}

func validateFilesystem(cfg FilesystemConfig) error {
	if !cfg.Enabled {
		if cfg.AllowMutations {
			return fmt.Errorf("tools.filesystem.allow_mutations requires tools.filesystem.enabled")
		}
		return nil
	}
	if cfg.MaxReadBytes <= 0 {
		return fmt.Errorf("tools.filesystem.max_read_bytes must be greater than zero")
	}
	if cfg.MaxWriteBytes <= 0 {
		return fmt.Errorf("tools.filesystem.max_write_bytes must be greater than zero")
	}
	if strings.TrimSpace(cfg.Root) == "" {
		return fmt.Errorf("tools.filesystem.root is required when filesystem tools are enabled")
	}
	if !filepath.IsAbs(cfg.Root) {
		return fmt.Errorf("tools.filesystem.root must be an absolute path")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(cfg.Root))
	if err != nil {
		return fmt.Errorf("tools.filesystem.root %q: %w", cfg.Root, err)
	}
	root = filepath.Clean(root)
	if root == filepath.VolumeName(root)+string(os.PathSeparator) {
		return fmt.Errorf("tools.filesystem.root must not be the filesystem root")
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("tools.filesystem.root %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("tools.filesystem.root %q is not a directory", root)
	}
	return nil
}

func validateWebSearch(cfg WebSearchConfig) error {
	switch cfg.Provider {
	case "duckduckgo":
		return nil
	case "brave":
		if strings.TrimSpace(cfg.APIKey) == "" {
			return fmt.Errorf("tools.web_search.api_key is required for provider brave")
		}
		return nil
	case "searxng":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return fmt.Errorf("tools.web_search.base_url is required for provider searxng")
		}
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("tools.web_search.base_url must be a valid HTTP(S) URL")
		}
		return nil
	default:
		return fmt.Errorf("tools.web_search.provider must be duckduckgo, brave, or searxng")
	}
}

func validateLogLevel(level string) error {
	switch level {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("log.level must be debug, info, warn, or error")
	}
}

func validateRegularFile(name, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s is required", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %q: %w", name, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s %q is not a regular file", name, path)
	}
	return nil
}

func validateDirectory(name, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s is required", name)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %q: %w", name, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %q is not a directory", name, path)
	}
	return nil
}

func validateExecutable(name, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !strings.ContainsRune(path, os.PathSeparator) {
		if _, err := exec.LookPath(path); err != nil {
			return fmt.Errorf("%s %q: %w", name, path, err)
		}
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %q: %w", name, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s %q is not a regular file", name, path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s %q is not executable", name, path)
	}
	return nil
}
