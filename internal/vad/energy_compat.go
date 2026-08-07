package vad

// Semantic aliases used by the Phase 2 coordinator-facing VAD contract. The
// energy detector keeps its detailed trailing-silence events for calibration
// and backwards-compatible tests.
const (
	EventSpeechStarted = EventVoiceDetected
	EventSpeechEnded   = EventCutBySilence
)

// SetEventHandler implements Detector for the legacy energy detector.
func (v *VAD) SetEventHandler(handler func(Event)) {
	if v == nil {
		return
	}
	v.OnEvent = handler
}

// Close implements Detector. The energy detector owns no native resources.
func (v *VAD) Close() error { return nil }

var _ Detector = (*VAD)(nil)
