// Package wake contains deterministic wake-word matching independent from STT.
package wake

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Detector decides whether a final transcript opens a voice turn.
type Detector interface {
	Detect(transcript string) (command string, matched bool)
}

// PhraseDetector matches a primary wake phrase plus aliases as complete words.
// It deliberately operates after STT so the audio is transcribed exactly once.
type PhraseDetector struct {
	phrases [][]string
}

// NewPhraseDetector validates and compiles the primary wake phrase and aliases.
func NewPhraseDetector(primary string, aliases []string) (*PhraseDetector, error) {
	inputs := append([]string{primary}, aliases...)
	phrases := make([][]string, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		words := normalizedWords(input)
		if len(words) == 0 {
			return nil, fmt.Errorf("wake phrase must contain at least one word")
		}
		key := strings.Join(words, " ")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		phrases = append(phrases, words)
	}
	return &PhraseDetector{phrases: phrases}, nil
}

// Detect accepts a wake phrase near the beginning (first three words) or as a
// trailing address (last two words). This allows "hola Xarlatan" and
// "qué hora es, Xarlatan" while rejecting most ambient mentions in the middle.
func (d *PhraseDetector) Detect(transcript string) (string, bool) {
	if d == nil || len(d.phrases) == 0 {
		return "", false
	}
	tokens := tokenize(transcript)
	for _, phrase := range d.phrases {
		if len(phrase) > len(tokens) {
			continue
		}
		for start := 0; start+len(phrase) <= len(tokens); start++ {
			if !tokenSequenceMatches(tokens, start, phrase) {
				continue
			}
			end := start + len(phrase)
			if start > 2 && len(tokens)-end > 1 {
				continue
			}
			parts := make([]string, 0, len(tokens)-len(phrase))
			for i, token := range tokens {
				if i >= start && i < end {
					continue
				}
				parts = append(parts, token.raw)
			}
			command := strings.Join(parts, " ")
			if command == "" {
				// Wake word alone remains a meaningful conversational input.
				command = strings.TrimSpace(transcript)
			}
			return command, true
		}
	}
	return "", false
}

type token struct {
	raw        string
	normalized string
}

func tokenize(text string) []token {
	var tokens []token
	start := -1
	for offset, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = offset
			}
			continue
		}
		if start >= 0 {
			raw := text[start:offset]
			tokens = append(tokens, token{raw: raw, normalized: normalizeWord(raw)})
			start = -1
		}
	}
	if start >= 0 {
		raw := text[start:]
		tokens = append(tokens, token{raw: raw, normalized: normalizeWord(raw)})
	}
	return tokens
}

func normalizedWords(text string) []string {
	tokens := tokenize(text)
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.normalized != "" {
			words = append(words, token.normalized)
		}
	}
	return words
}

func tokenSequenceMatches(tokens []token, start int, phrase []string) bool {
	for i, word := range phrase {
		if tokens[start+i].normalized != word {
			return false
		}
	}
	return true
}

func normalizeWord(word string) string {
	var b strings.Builder
	b.Grow(len(word))
	for len(word) > 0 {
		r, size := utf8.DecodeRuneInString(word)
		word = word[size:]
		r = unicode.ToLower(r)
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
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var _ Detector = (*PhraseDetector)(nil)
