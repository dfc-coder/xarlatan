package vad

// Detector is the audio-facing VAD boundary. Implementations decide whether
// incoming chunks belong to an active utterance and when that utterance ends.
// Audio capture depends only on this contract, not on a concrete VAD runtime.
type Detector interface {
	ProcessChunk([]float32) (isSpeaking bool, shouldFinish bool)
	Reset()
	IsSpeaking() bool
	SetEventHandler(func(Event))
	Close() error
}
