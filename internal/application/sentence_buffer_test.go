package application

import (
	"reflect"
	"strings"
	"testing"
)

func TestSentenceBufferReassemblesArbitraryDeltasIntoSentences(t *testing.T) {
	buffer := newSentenceBuffer()
	var phrases []string
	for _, delta := range []string{"Hola", ", ¿cómo", " estás? Bien", "."} {
		phrases = append(phrases, buffer.Push(delta)...)
	}
	phrases = append(phrases, buffer.Flush()...)
	want := []string{"Hola, ¿cómo estás?", "Bien."}
	if !reflect.DeepEqual(phrases, want) {
		t.Fatalf("phrases = %v, want %v", phrases, want)
	}
}

func TestSentenceBufferFlushesTrailingFragment(t *testing.T) {
	buffer := newSentenceBuffer()
	if got := buffer.Push("respuesta sin punto"); len(got) != 0 {
		t.Fatalf("Push() = %v, want no complete phrase", got)
	}
	if got := buffer.Flush(); !reflect.DeepEqual(got, []string{"respuesta sin punto"}) {
		t.Fatalf("Flush() = %v", got)
	}
	if got := buffer.Flush(); len(got) != 0 {
		t.Fatalf("second Flush() = %v, want empty", got)
	}
}

func TestSentenceBufferBoundsUnpunctuatedPhrase(t *testing.T) {
	buffer := &sentenceBuffer{maxRunes: 20}
	text := "uno dos tres cuatro cinco seis siete"
	phrases := buffer.Push(text)
	if len(phrases) == 0 {
		t.Fatal("long unpunctuated input did not emit a bounded phrase")
	}
	if len([]rune(phrases[0])) > 20 {
		t.Fatalf("first phrase runes = %d, want <= 20: %q", len([]rune(phrases[0])), phrases[0])
	}
	remaining := buffer.Flush()
	joined := strings.TrimSpace(strings.Join(append(phrases, remaining...), " "))
	if joined != text {
		t.Fatalf("reconstructed = %q, want %q", joined, text)
	}
}

func TestSentenceBufferHandlesUnicodeBoundary(t *testing.T) {
	buffer := &sentenceBuffer{maxRunes: 5}
	text := "áéíóúñandú"
	phrases := buffer.Push(text)
	phrases = append(phrases, buffer.Flush()...)
	if len(phrases) < 2 {
		t.Fatalf("phrases = %v, want bounded UTF-8 chunks", phrases)
	}
	for _, phrase := range phrases {
		if len([]rune(phrase)) > 5 {
			t.Fatalf("phrase exceeds rune bound: %q", phrase)
		}
	}
	if reconstructed := strings.Join(phrases, ""); reconstructed != text {
		t.Fatalf("reconstructed = %q, want %q", reconstructed, text)
	}
}
