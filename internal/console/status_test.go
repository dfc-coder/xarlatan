package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusPrinter_DeduplicatesRepeatedStatus(t *testing.T) {
	var buf bytes.Buffer
	p := NewStatusPrinter(&buf)

	p.Set("Escuchando")
	p.Set("Escuchando")
	p.Set("Pensando")

	got := buf.String()
	want := "Escuchando\nPensando\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestStatusPrinter_IgnoresEmptyStatus(t *testing.T) {
	var buf bytes.Buffer
	p := NewStatusPrinter(&buf)

	p.Set("")
	p.Set("Hablando")

	if strings.TrimSpace(buf.String()) != "Hablando" {
		t.Fatalf("output = %q, want Hablando", buf.String())
	}
}
