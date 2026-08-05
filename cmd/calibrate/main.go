// Command calibrate records a few seconds of ambient silence and prints
// a suggested silence_threshold for config.yaml based on the noise floor.
//
// Usage:
//
//	go run ./cmd/calibrate            # record from default ALSA device
//	go run ./cmd/calibrate -device hw:1,0 -duration 5
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"github.com/dfc-coder/xarlatan/internal/vad"
)

func main() {
	device := flag.String("device", "default", "ALSA capture device")
	duration := flag.Int("duration", 3, "seconds of ambient noise to sample")
	sampleRate := flag.Int("rate", 16000, "sample rate in Hz")
	flag.Parse()

	fmt.Fprintf(os.Stderr, "Recording %d s of ambient noise from device %q — stay quiet...\n", *duration, *device)

	args := []string{
		"-D", *device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", *sampleRate),
		"-c", "1",
		"-d", fmt.Sprintf("%d", *duration),
		"-t", "raw",
		"-q",
		"-",
	}

	cmd := exec.Command("arecord", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("arecord start: %v", err)
	}

	raw, err := io.ReadAll(stdout)
	if err != nil {
		log.Fatalf("read: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		log.Fatalf("arecord: %v", err)
	}

	samples := pcmToFloat32(raw)
	if len(samples) == 0 {
		log.Fatal("no audio captured")
	}

	chunkSize := *sampleRate * 80 / 1000 // 80 ms chunks, matching capture.go
	stats := vad.MeasureNoise(samples, chunkSize)
	suggested := stats.SuggestedThreshold()

	fmt.Fprintf(os.Stderr, "\nNoise floor — mean RMS: %.5f  stddev: %.5f  max: %.5f\n",
		stats.Mean, stats.StdDev, stats.Max)
	fmt.Fprintf(os.Stderr, "Suggested threshold (mean + 4σ, floor 0.005): %.5f\n\n", suggested)

	fmt.Printf("# Paste into config.yaml → audio section:\n")
	fmt.Printf("audio:\n")
	fmt.Printf("  silence_threshold: %.4f\n", suggested)
	fmt.Printf("  silence_duration_ms: 1500  # increase to 1800 for deliberate/slow speech\n")
}

func pcmToFloat32(raw []byte) []float32 {
	n := len(raw) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(raw[i*2:]))
		out[i] = float32(s) / 32768.0
	}
	return out
}
