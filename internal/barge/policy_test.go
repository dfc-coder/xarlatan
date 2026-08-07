package barge

import "testing"

func TestExplicitStopPolicyRequiresWakeAndStopVerb(t *testing.T) {
	policy := NewExplicitStopPolicy(fakeWakeMatcher{})
	cases := []struct {
		input string
		want  bool
	}{
		{"Xarlatan, para", true},
		{"Xarlatan, detente por favor", true},
		{"Xarlatan, cállate", true},
		{"para", false},
		{"Xarlatan, qué hora es", false},
		{"estoy aquí para ayudarte", false},
	}
	for _, tc := range cases {
		if got := policy.Confirm(tc.input); got != tc.want {
			t.Fatalf("Confirm(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestExplicitStopPolicyWithoutWakeMatcherRejects(t *testing.T) {
	if NewExplicitStopPolicy(nil).Confirm("Xarlatan para") {
		t.Fatal("nil wake matcher confirmed interruption")
	}
}

type fakeWakeMatcher struct{}

func (fakeWakeMatcher) Detect(transcript string) (string, bool) {
	const prefix = "Xarlatan, "
	if len(transcript) <= len(prefix) || transcript[:len(prefix)] != prefix {
		return "", false
	}
	return transcript[len(prefix):], true
}
