package stt

import (
	"strings"
	"unicode/utf8"
)

const maxNonVerbalMarkerRunes = 64

func filterTranscript(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || isNonVerbalMarker(trimmed) {
		return ""
	}
	return trimmed
}

func isNonVerbalMarker(text string) bool {
	runeCount := utf8.RuneCountInString(text)
	if runeCount < 2 || runeCount > maxNonVerbalMarkerRunes {
		return false
	}
	runes := []rune(text)
	var closing rune
	switch runes[0] {
	case '[':
		closing = ']'
	case '(':
		closing = ')'
	default:
		return false
	}
	return runes[len(runes)-1] == closing
}
