// Package barge contains deterministic policies for voice interruption.
package barge

import (
	"strings"
	"unicode"
)

// WakeMatcher is intentionally compatible with the wake.Detector contract
// without importing a concrete implementation.
type WakeMatcher interface {
	Detect(transcript string) (command string, matched bool)
}

// Policy decides whether a transcribed candidate is allowed to interrupt.
type Policy interface {
	Confirm(transcript string) bool
}

// ExplicitStopPolicy requires both the configured wake phrase and an explicit
// stop command. This prevents ordinary TTS echo from becoming a cancellation.
type ExplicitStopPolicy struct {
	wake WakeMatcher
}

func NewExplicitStopPolicy(wake WakeMatcher) *ExplicitStopPolicy {
	return &ExplicitStopPolicy{wake: wake}
}

func (p *ExplicitStopPolicy) Confirm(transcript string) bool {
	if p == nil || p.wake == nil {
		return false
	}
	command, matched := p.wake.Detect(transcript)
	if !matched {
		return false
	}
	words := normalizedWords(command)
	if len(words) == 0 {
		return false
	}
	_, ok := explicitStops[words[0]]
	return ok
}

var explicitStops = map[string]struct{}{
	"para":     {},
	"parate":   {},
	"detente":  {},
	"deten":    {},
	"cancela":  {},
	"cancelar": {},
	"basta":    {},
	"silencio": {},
	"callate":  {},
}

func normalizedWords(text string) []string {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		words = append(words, current.String())
		current.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		}
		current.WriteRune(r)
	}
	flush()
	return words
}

var _ Policy = (*ExplicitStopPolicy)(nil)
