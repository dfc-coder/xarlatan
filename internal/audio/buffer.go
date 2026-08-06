package audio

import "fmt"

// Buffer is the transport-independent audio value shared by voice boundaries.
// Samples are normalized float32 values in the range [-1, 1].
type Buffer struct {
	Samples    []float32
	SampleRate int
	Channels   int
}

// Clone returns a defensive copy.
func (b Buffer) Clone() Buffer {
	cloned := b
	if len(b.Samples) > 0 {
		cloned.Samples = append([]float32(nil), b.Samples...)
	}
	return cloned
}

// Empty reports whether the buffer contains no samples.
func (b Buffer) Empty() bool {
	return len(b.Samples) == 0
}

// Validate checks the minimum format required by the current voice pipeline.
func (b Buffer) Validate() error {
	if b.SampleRate <= 0 {
		return fmt.Errorf("audio sample rate must be greater than zero")
	}
	if b.Channels != 1 {
		return fmt.Errorf("audio channels must be mono")
	}
	return nil
}
