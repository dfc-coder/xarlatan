package stt

import "testing"

func TestFilterTranscriptRejectsNonVerbalMarkers(t *testing.T) {
	for _, input := range []string{
		"[Música]",
		"[Music]",
		"(Sombre)",
		" ( ruido ) ",
		"[]",
		"()",
	} {
		if got := filterTranscript(input); got != "" {
			t.Errorf("filterTranscript(%q) = %q, want empty", input, got)
		}
	}
}

func TestFilterTranscriptPreservesNormalSpeech(t *testing.T) {
	for _, input := range []string{
		"¿Qué día viene después del lunes?",
		"pon música",
		"hola (otra vez)",
		"usa [corchetes] aquí",
	} {
		if got := filterTranscript(input); got != input {
			t.Errorf("filterTranscript(%q) = %q", input, got)
		}
	}
}

func TestFilterTranscriptTrimsWhitespace(t *testing.T) {
	if got := filterTranscript("  hola  "); got != "hola" {
		t.Fatalf("filterTranscript() = %q, want hola", got)
	}
}
