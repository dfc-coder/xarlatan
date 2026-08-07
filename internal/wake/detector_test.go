package wake

import "testing"

func TestPhraseDetectorAcceptsPrimaryAndAliases(t *testing.T) {
	detector, err := NewPhraseDetector("xarlatan", []string{"charlatan", "charlatán"})
	if err != nil {
		t.Fatalf("NewPhraseDetector() error = %v", err)
	}
	cases := []struct {
		input string
		want  string
	}{
		{"Xarlatan, ¿qué hora es?", "qué hora es"},
		{"Hola Charlatán, busca vuelos", "Hola   busca vuelos"},
		{"qué puedes hacer, charlatan", "qué puedes hacer"},
	}
	for _, tc := range cases {
		got, ok := detector.Detect(tc.input)
		if !ok {
			t.Fatalf("Detect(%q) did not match", tc.input)
		}
		if got != tc.want {
			t.Fatalf("Detect(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPhraseDetectorRejectsSubstringAndAmbientMiddleMention(t *testing.T) {
	detector, err := NewPhraseDetector("xarlatan", []string{"charlatan"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		"xarlatanes no es la palabra",
		"ayer estuvimos hablando sobre xarlatan durante horas y seguimos",
		"hola qué tal",
	} {
		if command, ok := detector.Detect(input); ok {
			t.Fatalf("Detect(%q) matched unexpectedly with %q", input, command)
		}
	}
}

func TestPhraseDetectorWakeOnlyRemainsMeaningful(t *testing.T) {
	detector, err := NewPhraseDetector("xarlatan", nil)
	if err != nil {
		t.Fatal(err)
	}
	command, ok := detector.Detect("¡Xarlatan!")
	if !ok || command != "¡Xarlatan!" {
		t.Fatalf("Detect wake-only = (%q,%v)", command, ok)
	}
}

func TestPhraseDetectorRejectsEmptyPhrase(t *testing.T) {
	if _, err := NewPhraseDetector("", nil); err == nil {
		t.Fatal("NewPhraseDetector(empty) error = nil")
	}
}
