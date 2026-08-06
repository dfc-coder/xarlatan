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
	if utf8.RuneCountInString(text) < 2 || utf8.RuneCountInString(text) > maxNonVerbalMarkerRunes {
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
	if runes[len(runes)-1] != closing {
		return false
	}
	return strings.TrimSpace(string(runes[1:len(runes)-1])) != ""
}
