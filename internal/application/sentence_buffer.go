package application

import (
	"strings"
	"unicode/utf8"
)

const defaultMaxStreamingPhraseRunes = 180

type sentenceBuffer struct {
	pending  string
	maxRunes int
}

func newSentenceBuffer() *sentenceBuffer {
	return &sentenceBuffer{maxRunes: defaultMaxStreamingPhraseRunes}
}

// Push appends an arbitrary LLM delta and returns complete TTS-sized phrases.
// Strong sentence punctuation is preferred; a bounded rune cap prevents an
// unpunctuated model response from delaying audio indefinitely.
func (b *sentenceBuffer) Push(delta string) []string {
	if b == nil || delta == "" {
		return nil
	}
	b.pending += delta
	return b.takeReady(false)
}

func (b *sentenceBuffer) Flush() []string {
	if b == nil {
		return nil
	}
	return b.takeReady(true)
}

func (b *sentenceBuffer) takeReady(flush bool) []string {
	var phrases []string
	for {
		boundary := strongSentenceBoundary(b.pending)
		if boundary == 0 && utf8.RuneCountInString(b.pending) >= b.maxRunes {
			boundary = boundedPhraseBoundary(b.pending, b.maxRunes)
		}
		if boundary == 0 {
			break
		}
		phrase := strings.TrimSpace(b.pending[:boundary])
		b.pending = b.pending[boundary:]
		if phrase != "" {
			phrases = append(phrases, phrase)
		}
	}
	if flush {
		phrase := strings.TrimSpace(b.pending)
		b.pending = ""
		if phrase != "" {
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

func strongSentenceBoundary(text string) int {
	for index, r := range text {
		switch r {
		case '.', '!', '?', '\n':
			return index + utf8.RuneLen(r)
		}
	}
	return 0
}

func boundedPhraseBoundary(text string, maxRunes int) int {
	if maxRunes <= 0 {
		return len(text)
	}
	runes := 0
	lastSpaceByte := 0
	for index, r := range text {
		if runes >= maxRunes {
			if lastSpaceByte > 0 {
				return lastSpaceByte
			}
			return index
		}
		runes++
		if r == ' ' || r == '\t' {
			lastSpaceByte = index + utf8.RuneLen(r)
		}
	}
	return len(text)
}
