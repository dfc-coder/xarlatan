package vad

import "testing"

// warmup feeds n voice chunks to saturate the EMA above the release threshold.
// With alpha=0.25 and voice RMS=0.8, smoothedEnergy converges to ~0.8:
//
//	after 3 chunks: ≈0.46, after 5 chunks: ≈0.61.
//
// 5 chunks is enough for all tests that need stable speaking state.
func warmup(v *VAD, n int) {
	voice := []float32{0.8, 0.8, 0.8, 0.8}
	for i := 0; i < n; i++ {
		v.ProcessChunk(voice)
	}
}

func TestProcessChunk_ActivatesOnRawEnergy(t *testing.T) {
	// Activation uses raw RMS, so a single loud chunk triggers StateSpeaking immediately.
	v := New(0.5, 0.35, 800000000, 10)

	if isSpeaking, shouldFinish := v.ProcessChunk([]float32{0, 0, 0, 0}); isSpeaking || shouldFinish {
		t.Fatalf("silent = (%v, %v), want (false, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSilence {
		t.Fatalf("state = %v, want StateSilence", v.state)
	}
	if isSpeaking, shouldFinish := v.ProcessChunk([]float32{0.8, 0.8, 0.8, 0.8}); !isSpeaking || shouldFinish {
		t.Fatalf("voice = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state = %v, want StateSpeaking", v.state)
	}
}

func TestProcessChunk_EMAAbsorbsBriefEnergyDip(t *testing.T) {
	// With a warm EMA (5 voice chunks → smoothed ≈ 0.61), 1–2 quiet chunks do not
	// trigger StateTrailingSilence because the smoothed energy stays above releaseThreshold.
	// Only sustained silence (≥3 quiet chunks in these test params) causes the transition.
	v := New(0.5, 0.35, 800000000, 10)

	warmup(v, 5) // smoothed ≈ 0.61

	quiet := []float32{0.1, 0.1, 0.1, 0.1}

	// Brief dip — EMA absorbs it.
	if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || shouldFinish {
		t.Fatalf("quiet dip 1 = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state after dip 1 = %v, want StateSpeaking (EMA should absorb brief dip)", v.state)
	}

	// Second quiet chunk — still absorbed.
	if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || shouldFinish {
		t.Fatalf("quiet dip 2 = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state after dip 2 = %v, want StateSpeaking (EMA should absorb two-chunk dip)", v.state)
	}

	// Third quiet chunk — sustained silence finally crosses the release threshold.
	if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || shouldFinish {
		t.Fatalf("quiet dip 3 = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateTrailingSilence {
		t.Fatalf("state after dip 3 = %v, want StateTrailingSilence (sustained silence should trigger)", v.state)
	}
}

func TestProcessChunk_UsesHysteresisAndTrailingSilence(t *testing.T) {
	// Full lifecycle: silence → speaking (warmed) → soft speech stays speaking →
	// sustained quiet enters trailing silence → finish.
	v := New(0.5, 0.35, 800000000, 10)

	silent := []float32{0, 0, 0, 0}
	soft := []float32{0.45, 0.45, 0.45, 0.45}
	quiet := []float32{0.1, 0.1, 0.1, 0.1}

	// Pre-speech silence.
	if isSpeaking, shouldFinish := v.ProcessChunk(silent); isSpeaking || shouldFinish {
		t.Fatalf("silent = (%v, %v), want (false, false)", isSpeaking, shouldFinish)
	}

	// Warm up the EMA: 5 voice chunks (smoothed ≈ 0.61 → well above releaseThreshold=0.35).
	warmup(v, 5)
	if v.state != StateSpeaking {
		t.Fatalf("state after warmup = %v, want StateSpeaking", v.state)
	}

	// Soft speech (0.45 > 0.35) stays in StateSpeaking (upper-threshold hysteresis).
	if isSpeaking, shouldFinish := v.ProcessChunk(soft); !isSpeaking || shouldFinish {
		t.Fatalf("soft chunk = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state after soft = %v, want StateSpeaking", v.state)
	}

	// First two quiet chunks are absorbed by EMA — no transition yet.
	for i := 0; i < 2; i++ {
		if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || shouldFinish {
			t.Fatalf("quiet %d = (%v, %v), want (true, false)", i+1, isSpeaking, shouldFinish)
		}
		if v.state != StateSpeaking {
			t.Fatalf("state after quiet %d = %v, want StateSpeaking", i+1, v.state)
		}
	}

	// Third quiet chunk crosses the release threshold → enter StateTrailingSilence.
	if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || shouldFinish {
		t.Fatalf("quiet 3 = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateTrailingSilence {
		t.Fatalf("state after quiet 3 = %v, want StateTrailingSilence", v.state)
	}

	// Fourth quiet chunk fills maxSilence (8 samples = 2 chunks of 4 at sampleRate=10) → finish.
	if isSpeaking, shouldFinish := v.ProcessChunk(quiet); !isSpeaking || !shouldFinish {
		t.Fatalf("quiet 4 = (%v, %v), want (true, true)", isSpeaking, shouldFinish)
	}
	if v.state != StateSilence {
		t.Fatalf("state after finish = %v, want StateSilence", v.state)
	}
}

func TestProcessChunk_SoftSpeechAboveReleaseThresholdStaysSpeaking(t *testing.T) {
	// Soft speech with energy between releaseThreshold and threshold stays in StateSpeaking.
	v := New(0.5, 0.35, 800000000, 10)

	softSpeech := []float32{0.4, 0.4, 0.4, 0.4} // 0.35 < 0.4 < 0.5

	warmup(v, 5)

	if isSpeaking, shouldFinish := v.ProcessChunk(softSpeech); !isSpeaking || shouldFinish {
		t.Fatalf("soft speech = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state = %v, want StateSpeaking", v.state)
	}
}

func TestProcessChunk_ResumesFromTrailingSilenceOnVoice(t *testing.T) {
	// After entering StateTrailingSilence, a voice chunk resumes to StateSpeaking
	// before the silence timeout fires.
	v := New(0.5, 0.35, 800000000, 10)

	voice := []float32{0.8, 0.8, 0.8, 0.8}
	quiet := []float32{0.1, 0.1, 0.1, 0.1}

	warmup(v, 5) // smoothed ≈ 0.61

	// Feed 3 quiet chunks to enter StateTrailingSilence (silenceSamples=4, maxSilence=8).
	for i := 0; i < 3; i++ {
		v.ProcessChunk(quiet)
	}
	if v.state != StateTrailingSilence {
		t.Fatalf("state before resume = %v, want StateTrailingSilence", v.state)
	}

	// Voice resumes speaking before timeout.
	if isSpeaking, shouldFinish := v.ProcessChunk(voice); !isSpeaking || shouldFinish {
		t.Fatalf("resume = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state after resume = %v, want StateSpeaking", v.state)
	}
}

func TestProcessChunk_ContinuesOnSpeech(t *testing.T) {
	v := New(0.5, 0.35, 800000000, 10)

	voice := []float32{0.8, 0.8, 0.8, 0.8}

	if isSpeaking, shouldFinish := v.ProcessChunk(voice); !isSpeaking || shouldFinish {
		t.Fatalf("first voice = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if isSpeaking, shouldFinish := v.ProcessChunk(voice); !isSpeaking || shouldFinish {
		t.Fatalf("second voice = (%v, %v), want (true, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSpeaking {
		t.Fatalf("state = %v, want StateSpeaking", v.state)
	}
}

func TestProcessChunk_SilenceAndNoiseStayIdle(t *testing.T) {
	v := New(0.5, 0.35, 800000000, 10)

	if isSpeaking, shouldFinish := v.ProcessChunk([]float32{0, 0, 0, 0}); isSpeaking || shouldFinish {
		t.Fatalf("silence = (%v, %v), want (false, false)", isSpeaking, shouldFinish)
	}
	if isSpeaking, shouldFinish := v.ProcessChunk([]float32{0.1, 0.1, 0.1, 0.1}); isSpeaking || shouldFinish {
		t.Fatalf("noise = (%v, %v), want (false, false)", isSpeaking, shouldFinish)
	}
	if v.state != StateSilence {
		t.Fatalf("state = %v, want StateSilence", v.state)
	}
}

func TestReset_ClearsSessionState(t *testing.T) {
	v := New(0.5, 0.35, 800000000, 10)

	warmup(v, 5)

	quiet := []float32{0.1, 0.1, 0.1, 0.1}
	for i := 0; i < 3; i++ {
		v.ProcessChunk(quiet)
	}
	if v.state != StateTrailingSilence {
		t.Fatalf("state before reset = %v, want StateTrailingSilence", v.state)
	}
	if v.silenceSamples == 0 {
		t.Fatalf("silenceSamples before reset = %d, want > 0", v.silenceSamples)
	}

	v.Reset()

	if v.state != StateSilence {
		t.Fatalf("state after reset = %v, want StateSilence", v.state)
	}
	if v.silenceSamples != 0 {
		t.Fatalf("silenceSamples after reset = %d, want 0", v.silenceSamples)
	}
	if v.smoothedEnergy != 0 {
		t.Fatalf("smoothedEnergy after reset = %v, want 0", v.smoothedEnergy)
	}
	if v.IsSpeaking() {
		t.Fatalf("IsSpeaking after reset = true, want false")
	}
}

func TestOnEvent_FiresInCorrectSequence(t *testing.T) {
	v := New(0.5, 0.35, 800000000, 10)

	var events []EventType
	v.OnEvent = func(e Event) {
		events = append(events, e.Type)
	}

	voice := []float32{0.8, 0.8, 0.8, 0.8}
	quiet := []float32{0.1, 0.1, 0.1, 0.1}

	// First voice chunk → EventVoiceDetected (raw activation).
	v.ProcessChunk(voice)
	if len(events) != 1 || events[0] != EventVoiceDetected {
		t.Fatalf("after first voice: events = %v, want [EventVoiceDetected]", events)
	}

	// Warm up EMA so subsequent quiet triggers EventSilenceStart.
	warmup(v, 4)
	events = events[:0]

	// Feed quiet chunks until EventSilenceStart fires.
	for i := 0; i < 3; i++ {
		v.ProcessChunk(quiet)
	}

	if len(events) == 0 {
		t.Fatal("no events after quiet chunks, want at least EventSilenceStart")
	}
	if events[0] != EventSilenceStart {
		t.Fatalf("first event = %v, want EventSilenceStart", events[0])
	}
	// Subsequent events in trailing silence should be EventSilenceProgress.
	for _, et := range events[1:] {
		if et != EventSilenceProgress {
			t.Fatalf("trailing event = %v, want EventSilenceProgress", et)
		}
	}

	// One more quiet chunk completes maxSilence → EventCutBySilence.
	events = events[:0]
	v.ProcessChunk(quiet)
	if len(events) != 1 || events[0] != EventCutBySilence {
		t.Fatalf("cut event = %v, want [EventCutBySilence]", events)
	}
}

func TestOnEvent_SilenceMSIsPopulated(t *testing.T) {
	// sampleRate=10, chunkSize=4 → each chunk = 400 ms
	v := New(0.5, 0.35, 800000000, 10)

	var cutEvent Event
	v.OnEvent = func(e Event) {
		if e.Type == EventCutBySilence {
			cutEvent = e
		}
	}

	quiet := []float32{0.1, 0.1, 0.1, 0.1}

	warmup(v, 5)

	// Drive to finish (3 quiet to enter trail + 1 more to finish = 2 chunks in trail * 4 samples = 8 samples).
	for i := 0; i < 4; i++ {
		v.ProcessChunk(quiet)
	}

	if cutEvent.Type != EventCutBySilence {
		t.Fatal("EventCutBySilence not received")
	}
	if cutEvent.SilenceMS <= 0 {
		t.Fatalf("SilenceMS = %d, want > 0", cutEvent.SilenceMS)
	}
}

func TestMeasureNoise_ComputesStats(t *testing.T) {
	// Constant-amplitude signal → mean=amp, stddev≈0, max=amp.
	const amp = float32(0.01)
	samples := make([]float32, 1600)
	for i := range samples {
		samples[i] = amp
	}

	stats := MeasureNoise(samples, 160)

	want := float64(amp)
	if diff := stats.Mean - want; diff < -0.001 || diff > 0.001 {
		t.Fatalf("Mean = %v, want ≈ %v", stats.Mean, want)
	}
	if stats.StdDev > 0.001 {
		t.Fatalf("StdDev = %v, want ≈ 0 for constant signal", stats.StdDev)
	}
	if diff := stats.Max - want; diff < -0.001 || diff > 0.001 {
		t.Fatalf("Max = %v, want ≈ %v", stats.Max, want)
	}
}

func TestMeasureNoise_SuggestedThresholdFloor(t *testing.T) {
	// Near-zero noise → SuggestedThreshold should be at least 0.005.
	stats := NoiseStats{Mean: 0.0001, StdDev: 0.0001, Max: 0.001}
	got := stats.SuggestedThreshold()
	if got < 0.005 {
		t.Fatalf("SuggestedThreshold = %v, want >= 0.005", got)
	}
}

func TestMeasureNoise_EmptyInput(t *testing.T) {
	stats := MeasureNoise(nil, 160)
	if stats.Mean != 0 || stats.StdDev != 0 || stats.Max != 0 {
		t.Fatalf("empty input stats = %+v, want zero", stats)
	}
}
