package audio

type preRollBuffer struct {
	maxChunks int
	chunks    [][]float32
}

func newPreRollBuffer(maxChunks int) *preRollBuffer {
	if maxChunks < 0 {
		maxChunks = 0
	}
	return &preRollBuffer{maxChunks: maxChunks}
}

func (b *preRollBuffer) add(chunk []float32) {
	if b == nil || len(chunk) == 0 {
		return
	}
	if b.maxChunks == 0 {
		return
	}
	copyChunk := append([]float32(nil), chunk...)
	b.chunks = append(b.chunks, copyChunk)
	if len(b.chunks) > b.maxChunks {
		trim := len(b.chunks) - b.maxChunks
		copy(b.chunks, b.chunks[trim:])
		b.chunks = b.chunks[:b.maxChunks]
	}
}

func (b *preRollBuffer) startRecording(chunk []float32) []float32 {
	if b == nil {
		return append([]float32(nil), chunk...)
	}
	out := make([]float32, 0, len(chunk))
	for _, prev := range b.chunks {
		out = append(out, prev...)
	}
	out = append(out, chunk...)
	return out
}

func (b *preRollBuffer) reset() {
	if b == nil {
		return
	}
	b.chunks = b.chunks[:0]
}
