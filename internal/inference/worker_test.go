package inference

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dfc-coder/xarlatan/internal/audio"
)

func TestWorkerPersistsAcrossRequests(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "starts")
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          writeFakeWorker(t),
		Mode:            "stt",
		RequestedDevice: "GPU",
		FallbackDevice:  "CPU",
		Env:             []string{"XARLATAN_TEST_START_COUNTER=" + counter},
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartWorker() error = %v", err)
	}
	defer worker.Close()

	for i := 0; i < 2; i++ {
		text, err := worker.Transcribe(context.Background(), audio.Buffer{Samples: []float32{0.1}, SampleRate: 16000, Channels: 1})
		if err != nil {
			t.Fatalf("Transcribe() error = %v", err)
		}
		if text != "transcribed" {
			t.Fatalf("text = %q", text)
		}
	}
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("worker starts = %d, want 1", got)
	}
}

func TestWorkerCancellationReapsAndRestarts(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "starts")
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          writeFakeWorker(t),
		Mode:            "tts",
		RequestedDevice: "GPU",
		FallbackDevice:  "CPU",
		Env: []string{
			"XARLATAN_TEST_START_COUNTER=" + counter,
			"XARLATAN_TEST_BLOCK_TEXT=block",
		},
		ShutdownTimeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("StartWorker() error = %v", err)
	}
	defer worker.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := worker.Synthesize(ctx, "block"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Synthesize(block) error = %v, want deadline", err)
	}

	buffer, err := worker.Synthesize(context.Background(), "hola")
	if err != nil {
		t.Fatalf("Synthesize(recovery) error = %v", err)
	}
	if buffer.SampleRate != 24000 || len(buffer.Samples) == 0 {
		t.Fatalf("recovery buffer = %+v", buffer)
	}
	if got := readCounter(t, counter); got != 2 {
		t.Fatalf("worker starts = %d, want 2 after cancellation restart", got)
	}
}

func TestWorkerHealthExposesDeviceFallback(t *testing.T) {
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          writeFakeWorker(t),
		Mode:            "stt",
		RequestedDevice: "GPU",
		FallbackDevice:  "CPU",
		Env:             []string{"XARLATAN_TEST_RESOLVED_DEVICE=CPU"},
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartWorker() error = %v", err)
	}
	defer worker.Close()

	health, err := worker.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.RequestedDevice != "GPU" || health.ResolvedDevice != "CPU" || !strings.Contains(health.FallbackReason, "GPU") {
		t.Fatalf("health = %+v", health)
	}
}

func TestWorkerCloseReapsChildAndIsIdempotent(t *testing.T) {
	worker, err := StartWorker(context.Background(), WorkerConfig{
		Python:          "python3",
		Script:          writeFakeWorker(t),
		Mode:            "stt",
		RequestedDevice: "CPU",
		FallbackDevice:  "CPU",
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartWorker() error = %v", err)
	}
	if err := worker.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if worker.cmd == nil || worker.cmd.ProcessState == nil || !worker.cmd.ProcessState.Exited() {
		t.Fatalf("worker child was not reaped: %#v", worker.cmd)
	}
	if err := worker.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func writeFakeWorker(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake_voice_worker.py")
	script := `#!/usr/bin/env python3
import json
import os
import struct
import sys
import time

counter = os.environ.get("XARLATAN_TEST_START_COUNTER")
if counter:
    try:
        with open(counter, "r", encoding="utf-8") as f:
            count = int(f.read().strip() or "0")
    except FileNotFoundError:
        count = 0
    with open(counter, "w", encoding="utf-8") as f:
        f.write(str(count + 1))

requested = "CPU"
fallback = "CPU"
for i, arg in enumerate(sys.argv):
    if arg == "--device" and i + 1 < len(sys.argv):
        requested = sys.argv[i + 1]
    if arg == "--fallback-device" and i + 1 < len(sys.argv):
        fallback = sys.argv[i + 1]
resolved = os.environ.get("XARLATAN_TEST_RESOLVED_DEVICE", requested)
reason = "" if resolved == requested else f"{requested} unavailable; using {resolved}"
block_text = os.environ.get("XARLATAN_TEST_BLOCK_TEXT", "")

reader = sys.stdin.buffer
writer = sys.stdout.buffer
while True:
    line = reader.readline()
    if not line:
        break
    header = json.loads(line)
    size = int(header.get("payload_bytes", 0))
    payload = reader.read(size) if size else b""
    op = header.get("op")
    response = {"id": header["id"], "ok": True, "requested_device": requested, "resolved_device": resolved, "fallback_reason": reason}
    out = b""
    if op == "transcribe":
        response["text"] = "transcribed"
    elif op == "synthesize":
        if header.get("text") == block_text:
            time.sleep(60)
        response["sample_rate"] = 24000
        response["channels"] = 1
        out = struct.pack("<fff", 0.1, -0.2, 0.3)
    elif op != "health":
        response = {"id": header["id"], "ok": False, "error": "unknown op"}
    response["payload_bytes"] = len(out)
    writer.write((json.dumps(response) + "\n").encode())
    if out:
        writer.write(out)
    writer.flush()
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake worker: %v", err)
	}
	return path
}

func readCounter(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse counter: %v", err)
	}
	return value
}
