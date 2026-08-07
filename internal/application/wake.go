package application

import "strings"

// StateWakeDetected marks a transcript that passed the configured wake gate.
const StateWakeDetected State = "wake_detected"

// WakeDetector receives one final STT result and returns the command with the
// wake phrase removed. It carries text only inside the private data plane.
type WakeDetector interface {
	Detect(transcript string) (command string, matched bool)
}

// SetWakeDetector enables wake gating. A nil detector preserves the legacy
// always-accept behavior for tests and compatibility entry points.
func (c *Coordinator) SetWakeDetector(detector WakeDetector) {
	if c == nil {
		return
	}
	c.wake = detector
}

func (c *Coordinator) applyWakeGate(transcript string, recorder *turnRecorder, result *Result) (string, bool) {
	if c == nil || c.wake == nil {
		return transcript, true
	}
	command, matched := c.wake.Detect(transcript)
	if !matched {
		if result != nil {
			result.Noop = true
			result.Trace.Outcome = "wake_ignored"
		}
		if recorder != nil {
			recorder.emit(StateIdle)
		}
		return "", false
	}
	if recorder != nil {
		recorder.emit(StateWakeDetected)
	}
	return strings.TrimSpace(command), true
}
