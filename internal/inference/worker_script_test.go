package inference

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestRepositoryVoiceWorkerImplementsSTTProtocol(t *testing.T) {
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          repositoryWorkerScript(t),
		Mode:            "stt",
		RequestedDevice: "GPU",
		FallbackDevice:  "CPU",
		Language:        "es",
		Env:             []string{"XARLATAN_VOICE_WORKER_FAKE=1", "XARLATAN_FAKE_RESOLVED_DEVICE=CPU"},
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartWorker(stt) error = %v", err)
	}
	defer worker.Close()

	health, err := worker.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.RequestedDevice != "GPU" || health.ResolvedDevice != "CPU" || health.FallbackReason == "" {
		t.Fatalf("health = %+v", health)
	}
	text, err := worker.Transcribe(context.Background(), audio.Buffer{Samples: []float32{0.1, -0.1}, SampleRate: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if text != "transcripción de prueba" {
		t.Fatalf("text = %q", text)
	}
}

func TestRepositoryVoiceWorkerImplementsKokoroProtocol(t *testing.T) {
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          repositoryWorkerScript(t),
		Mode:            "tts",
		RequestedDevice: "GPU",
		FallbackDevice:  "CPU",
		Language:        "es",
		VoiceFile:       "ef_dora.bin",
		Env:             []string{"XARLATAN_VOICE_WORKER_FAKE=1"},
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartWorker(tts) error = %v", err)
	}
	defer worker.Close()

	buffer, err := worker.Synthesize(context.Background(), "Hola desde Kokoro")
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if buffer.SampleRate != 24000 || buffer.Channels != 1 || len(buffer.Samples) != 3 {
		t.Fatalf("buffer = %+v", buffer)
	}
}

func repositoryWorkerScript(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "scripts", "openvino_voice_worker.py"))
}
