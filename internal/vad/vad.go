// Package vad implements a simple energy-based Voice Activity Detector.
package vad

import (
	"math"
	"time"
)

// State represents the current VAD state machine state.
type State int

const (
	StateSilence State = iota
	StateSpeaking
	StateTrailingSilence
)

// EventType identifies which VAD lifecycle event occurred.
type EventType int

const (
	EventVoiceDetected   EventType = iota // StateSilence → StateSpeaking
	EventSilenceStart                     // StateSpeaking → StateTrailingSilence
	EventSilenceProgress                  // each chunk while in StateTrailingSilence
	EventCutBySilence                     // silence timeout reached; shouldFinish = true
)

// Event carries state-change notifications from the VAD to the caller.
type Event struct {
	Type      EventType
	SilenceMS int // accumulated trailing silence in ms (SilenceProgress and CutBySilence)
}

// NoiseStats summarises per-chunk RMS measurements over a noise sample.
type NoiseStats struct {
	Mean   float64
	StdDev float64
	Max    float64
}

// SuggestedThreshold returns mean + 4σ, floored at 0.005.
func (n NoiseStats) SuggestedThreshold() float64 {
	t := n.Mean + 4*n.StdDev
	if t < 0.005 {
		t = 0.005
	}
	return t
}

// VAD detects speech segments based on EMA-smoothed RMS energy.
type VAD struct {
	threshold        float64
	releaseThreshold float64
	silenceDuration  time.Duration
	sampleRate       int

	// EMA smoothing — prevents momentary energy dips from falsely triggering trailing-silence.
	emaAlpha       float64
	smoothedEnergy float64

	// Optional event callback; nil = no-op.
	OnEvent func(Event)

	state          State
	silenceSamples int
	maxSilence     int // sample count at which trailing silence ends recording
}

// New creates a VAD with separate activation and release thresholds.
// EMA alpha defaults to 0.25 (~4-chunk / 320 ms smoothing window at 80 ms chunks).
func New(threshold, releaseThreshold float64, silenceDuration time.Duration, sampleRate int) *VAD {
	samplesPerSecond := float64(sampleRate)
	maxSilence := int(silenceDuration.Seconds() * samplesPerSecond)
	if releaseThreshold < 0 {
		releaseThreshold = 0
	}
	if releaseThreshold > threshold {
		releaseThreshold = threshold
	}
	return &VAD{
		threshold:        threshold,
		releaseThreshold: releaseThreshold,
		silenceDuration:  silenceDuration,
		sampleRate:       sampleRate,
		maxSilence:       maxSilence,
		emaAlpha:         0.25,
		state:            StateSilence,
	}
}

// ProcessChunk evaluates a chunk of float32 samples.
// Returns (isSpeaking bool, shouldFinish bool).
//   - isSpeaking: the chunk should be appended to the recording buffer.
//   - shouldFinish: silence timeout reached; caller should stop recording.
func (v *VAD) ProcessChunk(samples []float32) (isSpeaking bool, shouldFinish bool) {
	raw := rms(samples)
	v.smoothedEnergy = v.emaAlpha*raw + (1-v.emaAlpha)*v.smoothedEnergy

	switch v.state {
	case StateSilence:
		// Use raw energy for fast speech-onset detection.
		if raw > v.threshold {
			v.state = StateSpeaking
			v.silenceSamples = 0
			v.emit(Event{Type: EventVoiceDetected})
			return true, false
		}
		return false, false

	case StateSpeaking:
		// Use smoothed energy so brief dips mid-word don't trigger trailing-silence.
		if v.smoothedEnergy <= v.releaseThreshold {
			v.state = StateTrailingSilence
			v.silenceSamples = len(samples)
			v.emit(Event{Type: EventSilenceStart})
			return true, false // still append trailing silence
		}
		return true, false

	case StateTrailingSilence:
		if v.smoothedEnergy > v.releaseThreshold {
			// Speech resumed before the silence tail completed.
			v.state = StateSpeaking
			v.silenceSamples = 0
			return true, false
		}
		v.silenceSamples += len(samples)
		silenceMS := v.silenceSamples * 1000 / v.sampleRate
		if v.silenceSamples >= v.maxSilence {
			v.emit(Event{Type: EventCutBySilence, SilenceMS: silenceMS})
			v.Reset()
			return true, true // include last chunk then stop
		}
		v.emit(Event{Type: EventSilenceProgress, SilenceMS: silenceMS})
		return true, false
	}

	return false, false
}

// Reset resets the VAD state machine and clears smoothed energy.
func (v *VAD) Reset() {
	v.state = StateSilence
	v.silenceSamples = 0
	v.smoothedEnergy = 0
}

// IsSpeaking returns true when a speech segment is active.
func (v *VAD) IsSpeaking() bool {
	return v.state != StateSilence
}

// MeasureNoise computes per-chunk RMS statistics from a slice of ambient samples.
// Feed it a few seconds of background audio (no speech) to calibrate thresholds.
func MeasureNoise(samples []float32, chunkSize int) NoiseStats {
	if chunkSize <= 0 || len(samples) < chunkSize {
		return NoiseStats{}
	}

	var energies []float64
	for i := 0; i+chunkSize <= len(samples); i += chunkSize {
		energies = append(energies, rms(samples[i:i+chunkSize]))
	}
	if len(energies) == 0 {
		return NoiseStats{}
	}

	var sum, maxVal float64
	for _, e := range energies {
		sum += e
		if e > maxVal {
			maxVal = e
		}
	}
	mean := sum / float64(len(energies))

	var variance float64
	for _, e := range energies {
		d := e - mean
		variance += d * d
	}
	variance /= float64(len(energies))

	return NoiseStats{
		Mean:   mean,
		StdDev: math.Sqrt(variance),
		Max:    maxVal,
	}
}

// emit calls OnEvent if set.
func (v *VAD) emit(e Event) {
	if v.OnEvent != nil {
		v.OnEvent(e)
	}
}

// rms computes Root Mean Square energy of a float32 slice.
func rms(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}
