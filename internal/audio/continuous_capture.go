package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/vad"
)

const (
	defaultContinuousPreRollChunks = 4
	defaultUtteranceQueueDepth     = 4
	defaultBargeCandidateDepth     = 2
	defaultPartialSnapshotDepth    = 1
	partialPreviewDuration         = time.Second
	bargePreRollChunks             = 2
	bargeWarmupChunks              = 3
	bargeTriggerChunks             = 2
	bargeRelativeGain              = 1.65
	bargeAbsoluteRMS               = 0.025
	bargeBaselineAlpha             = 0.08
	bargeMaxDuration               = 2 * time.Second
)

// ContinuousOptions bounds the only retained raw-audio windows in continuous
// capture. Both values are counts, so memory usage is deterministic.
type ContinuousOptions struct {
	PreRollChunks int
	QueueDepth    int
}

func defaultContinuousOptions() ContinuousOptions {
	return ContinuousOptions{
		PreRollChunks: defaultContinuousPreRollChunks,
		QueueDepth:    defaultUtteranceQueueDepth,
	}
}

// BargeCandidate is private data-plane audio captured while the assistant is
// speaking. It is intentionally bounded and never emitted as an observer event.
type BargeCandidate struct {
	Buffer    Buffer
	StartedAt time.Time
	PeakRMS   float64
}

// PartialAudio is a single bounded snapshot captured before a normal utterance
// reaches the Silero endpoint. It is private data-plane input to preview STT.
type PartialAudio struct {
	Buffer     Buffer
	CapturedAt time.Time
}

type captureProcess interface {
	io.Reader
	Stop() error
}

type captureProcessFactory interface {
	Start(config.AudioConfig) (captureProcess, error)
}

type execCaptureFactory struct{}

func (execCaptureFactory) Start(cfg config.AudioConfig) (captureProcess, error) {
	args := []string{
		"-D", cfg.Device,
		"-f", "S16_LE",
		"-r", fmt.Sprintf("%d", cfg.SampleRate),
		"-c", fmt.Sprintf("%d", cfg.Channels),
		"-t", "raw",
		"--buffer-size=2048",
		"-q",
		"-",
	}
	cmd := exec.Command("arecord", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("arecord pipe: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("arecord start: %w", err)
	}
	process := &execCaptureProcess{
		cmd:    cmd,
		stdout: stdout,
		done:   make(chan error, 1),
	}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

type execCaptureProcess struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	done     chan error
	stopOnce sync.Once
	stopErr  error
}

func (p *execCaptureProcess) Read(buffer []byte) (int, error) {
	if p == nil || p.stdout == nil {
		return 0, io.EOF
	}
	return p.stdout.Read(buffer)
}

func (p *execCaptureProcess) Stop() error {
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		if p.stdout != nil {
			_ = p.stdout.Close()
		}
		if p.cmd != nil && p.cmd.Process != nil {
			if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.stopErr = err
			}
		}
		if p.done != nil {
			<-p.done
		}
	})
	return p.stopErr
}

// ContinuousRecorder owns one long-lived microphone process and converts the
// endless PCM stream into bounded utterances. Turn cancellation only stops a
// wait on Next; the microphone remains owned by the recorder until Close.
type ContinuousRecorder struct {
	cfg     config.AudioConfig
	vad     vad.Detector
	preRoll *preRollBuffer
	factory captureProcessFactory

	utterances      chan Buffer
	partials        chan PartialAudio
	bargeCandidates chan BargeCandidate
	sourceErr       chan error

	mu      sync.Mutex
	process captureProcess
	running bool
	closed  bool

	wg         sync.WaitGroup
	closeOnce  sync.Once
	closeErr   error
	dropped    atomic.Uint64
	suppressed atomic.Bool
	waiting    atomic.Bool
}

// NewContinuousRecorderWithDetector constructs the production continuous
// capture source using bounded defaults (320ms pre-roll at 80ms chunks, four
// queued utterances). No process is started until the first Next call.
func NewContinuousRecorderWithDetector(cfg config.AudioConfig, detector vad.Detector) (*ContinuousRecorder, error) {
	return newContinuousRecorder(cfg, detector, defaultContinuousOptions(), execCaptureFactory{})
}

func newContinuousRecorder(
	cfg config.AudioConfig,
	detector vad.Detector,
	options ContinuousOptions,
	factory captureProcessFactory,
) (*ContinuousRecorder, error) {
	if detector == nil {
		return nil, fmt.Errorf("voice activity detector is nil")
	}
	if factory == nil {
		return nil, fmt.Errorf("capture process factory is nil")
	}
	if _, err := pcmChunkBytes(cfg.SampleRate, cfg.Channels); err != nil {
		return nil, err
	}
	if options.PreRollChunks <= 0 {
		return nil, fmt.Errorf("continuous pre-roll chunks must be greater than zero")
	}
	if options.QueueDepth <= 0 {
		return nil, fmt.Errorf("continuous utterance queue depth must be greater than zero")
	}
	detector.SetEventHandler(logVADEvent)
	return &ContinuousRecorder{
		cfg:             cfg,
		vad:             detector,
		preRoll:         newPreRollBuffer(options.PreRollChunks),
		factory:         factory,
		utterances:      make(chan Buffer, options.QueueDepth),
		partials:        make(chan PartialAudio, defaultPartialSnapshotDepth),
		bargeCandidates: make(chan BargeCandidate, defaultBargeCandidateDepth),
		sourceErr:       make(chan error, 1),
	}, nil
}

// Next waits for one completed utterance. Cancelling ctx does not stop the
// underlying microphone, which is deliberately scoped to ContinuousRecorder.
func (r *ContinuousRecorder) Next(ctx context.Context) (Buffer, error) {
	if r == nil {
		return Buffer{}, fmt.Errorf("continuous recorder is nil")
	}
	ctx = nonNilCaptureContext(ctx)
	if err := ctx.Err(); err != nil {
		return Buffer{}, err
	}
	r.resetPartials()
	r.waiting.Store(true)
	defer r.waiting.Store(false)

	// Prefer already-buffered speech before deciding whether the source needs
	// restart. This preserves a completed utterance if arecord ended immediately
	// after publishing it and avoids a redundant capture process.
	select {
	case buffer := <-r.utterances:
		return buffer.Clone(), nil
	default:
	}
	if err := r.ensureRunning(); err != nil {
		return Buffer{}, err
	}

	for {
		select {
		case <-ctx.Done():
			return Buffer{}, ctx.Err()
		case buffer := <-r.utterances:
			return buffer.Clone(), nil
		case err := <-r.sourceErr:
			if err != nil {
				return Buffer{}, err
			}
		}
	}
}

// PartialAudio exposes a single-slot private preview stream. Snapshots are
// produced only while one Next call is actively waiting for the current turn.
func (r *ContinuousRecorder) PartialAudio() <-chan PartialAudio {
	if r == nil {
		return nil
	}
	return r.partials
}

// BargeCandidates exposes a bounded private audio stream for the barge-in
// controller. Candidates are produced only while SetSuppressed(true) is active.
func (r *ContinuousRecorder) BargeCandidates() <-chan BargeCandidate {
	if r == nil {
		return nil
	}
	return r.bargeCandidates
}

// SetSuppressed is retained as the WI-10D compatibility name. In WI-11C it
// means "assistant is speaking": normal utterances remain suppressed, while a
// stricter acoustic path may publish bounded barge-in candidates for textual
// confirmation. Returning to false flushes stale candidates.
func (r *ContinuousRecorder) SetSuppressed(suppressed bool) {
	if r == nil {
		return
	}
	r.suppressed.Store(suppressed)
	if !suppressed {
		r.flushBargeCandidates()
	}
}

// DroppedUtterances reports how many oldest queued utterances were discarded
// to keep memory bounded when downstream processing falls behind capture.
func (r *ContinuousRecorder) DroppedUtterances() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

func (r *ContinuousRecorder) ensureRunning() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("continuous recorder is closed")
	}
	if r.running {
		return nil
	}
	process, err := r.factory.Start(r.cfg)
	if err != nil {
		return err
	}
	r.process = process
	r.running = true
	r.vad.Reset()
	r.preRoll.reset()
	r.wg.Add(1)
	go r.readLoop(process)
	return nil
}

func (r *ContinuousRecorder) readLoop(process captureProcess) {
	defer r.wg.Done()
	bytesPerChunk, err := pcmChunkBytes(r.cfg.SampleRate, r.cfg.Channels)
	if err != nil {
		r.sourceFailed(process, err)
		return
	}
	buffer := make([]byte, bytesPerChunk)
	var recording []float32
	partialSent := false
	wasSuppressed := r.suppressed.Load()
	barge := newBargeCaptureState()

	for {
		n, readErr := io.ReadFull(process, buffer)
		completeBytes := n - n%(pcmBytesPerSample*r.cfg.Channels)
		if completeBytes > 0 {
			chunk := pcmToFloat32(buffer[:completeBytes])
			if r.suppressed.Load() {
				if !wasSuppressed {
					r.vad.Reset()
					r.preRoll.reset()
					barge.reset()
					wasSuppressed = true
				}
				recording = nil
				partialSent = false
				r.resetPartials()
				if candidate, ready := r.processBargeChunk(barge, chunk); ready {
					r.publishBargeCandidate(candidate)
					r.vad.Reset()
					barge.reset()
				}
			} else {
				if wasSuppressed {
					r.vad.Reset()
					r.preRoll.reset()
					barge.reset()
					wasSuppressed = false
				}
				var finish bool
				recording, finish = r.processChunk(recording, chunk)
				partialSent = r.maybePublishPartial(recording, partialSent)
				if finish {
					r.publishUtterance(recording)
					recording = nil
					partialSent = false
					r.vad.Reset()
					r.preRoll.reset()
				}
			}
		}
		if readErr != nil {
			if r.isClosed() {
				return
			}
			if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
				r.sourceFailed(process, fmt.Errorf("continuous arecord ended: %w", readErr))
				return
			}
			r.sourceFailed(process, fmt.Errorf("continuous audio read: %w", readErr))
			return
		}
	}
}

func (r *ContinuousRecorder) processChunk(recording, chunk []float32) ([]float32, bool) {
	isSpeaking, shouldFinish := r.vad.ProcessChunk(chunk)
	if isSpeaking {
		if len(recording) == 0 {
			recording = r.preRoll.startRecording(chunk)
			r.preRoll.reset()
		} else {
			recording = append(recording, chunk...)
		}
	} else if len(recording) == 0 {
		r.preRoll.add(chunk)
	}
	return recording, shouldFinish
}

type bargeCaptureState struct {
	baseline   float64
	warmup     int
	loudChunks int
	startedAt  time.Time
	peakRMS    float64
	recording  []float32
	preRoll    *preRollBuffer
}

func newBargeCaptureState() *bargeCaptureState {
	state := &bargeCaptureState{preRoll: newPreRollBuffer(bargePreRollChunks)}
	state.reset()
	return state
}

func (s *bargeCaptureState) reset() {
	if s == nil {
		return
	}
	s.baseline = 0
	s.warmup = 0
	s.loudChunks = 0
	s.startedAt = time.Time{}
	s.peakRMS = 0
	s.recording = nil
	if s.preRoll != nil {
		s.preRoll.reset()
	}
}

func (r *ContinuousRecorder) processBargeChunk(state *bargeCaptureState, chunk []float32) (BargeCandidate, bool) {
	if state == nil || len(chunk) == 0 {
		return BargeCandidate{}, false
	}
	rms := chunkRMS(chunk)
	isSpeaking, shouldFinish := r.vad.ProcessChunk(chunk)

	if len(state.recording) == 0 {
		if state.warmup < bargeWarmupChunks {
			state.baseline = updateBargeBaseline(state.baseline, rms)
			state.warmup++
			state.preRoll.add(chunk)
			return BargeCandidate{}, false
		}
		threshold := math.Max(bargeAbsoluteRMS, state.baseline*bargeRelativeGain)
		if isSpeaking && rms >= threshold {
			state.loudChunks++
			if rms > state.peakRMS {
				state.peakRMS = rms
			}
			if state.loudChunks >= bargeTriggerChunks {
				preRollDuration := time.Duration(bargePreRollChunks*chunkDurationMS) * time.Millisecond
				state.startedAt = time.Now().Add(-preRollDuration)
				state.recording = state.preRoll.startRecording(chunk)
				state.preRoll.reset()
				return BargeCandidate{}, false
			}
			state.preRoll.add(chunk)
			return BargeCandidate{}, false
		}

		state.loudChunks = 0
		state.baseline = updateBargeBaseline(state.baseline, rms)
		state.preRoll.add(chunk)
		return BargeCandidate{}, false
	}

	state.recording = append(state.recording, chunk...)
	if rms > state.peakRMS {
		state.peakRMS = rms
	}
	maxSamples := int(float64(r.cfg.SampleRate*r.cfg.Channels) * bargeMaxDuration.Seconds())
	if !shouldFinish && len(state.recording) < maxSamples {
		return BargeCandidate{}, false
	}
	buffer := Buffer{
		Samples:    append([]float32(nil), state.recording...),
		SampleRate: r.cfg.SampleRate,
		Channels:   r.cfg.Channels,
	}
	return BargeCandidate{Buffer: buffer, StartedAt: state.startedAt, PeakRMS: state.peakRMS}, true
}

func updateBargeBaseline(current, sample float64) float64 {
	if current <= 0 {
		return sample
	}
	return current*(1-bargeBaselineAlpha) + sample*bargeBaselineAlpha
}

func chunkRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range samples {
		v := float64(sample)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func (r *ContinuousRecorder) maybePublishPartial(recording []float32, alreadySent bool) bool {
	if alreadySent || r == nil || !r.waiting.Load() || len(recording) == 0 {
		return alreadySent
	}
	threshold := int(float64(r.cfg.SampleRate*r.cfg.Channels) * partialPreviewDuration.Seconds())
	if len(recording) < threshold {
		return false
	}
	r.publishPartial(PartialAudio{
		Buffer: Buffer{
			Samples:    append([]float32(nil), recording...),
			SampleRate: r.cfg.SampleRate,
			Channels:   r.cfg.Channels,
		},
		CapturedAt: time.Now(),
	})
	return true
}

func (r *ContinuousRecorder) publishPartial(preview PartialAudio) {
	if r == nil || r.partials == nil || preview.Buffer.Empty() {
		return
	}
	select {
	case r.partials <- preview:
		return
	default:
	}
	select {
	case <-r.partials:
	default:
	}
	select {
	case r.partials <- preview:
	default:
	}
}

func (r *ContinuousRecorder) resetPartials() {
	if r == nil || r.partials == nil {
		return
	}
	for {
		select {
		case <-r.partials:
		default:
			return
		}
	}
}

func (r *ContinuousRecorder) publishUtterance(samples []float32) {
	minimumSamples := r.cfg.SampleRate * r.cfg.Channels / 4
	if len(samples) < minimumSamples {
		return
	}
	buffer := Buffer{
		Samples:    append([]float32(nil), samples...),
		SampleRate: r.cfg.SampleRate,
		Channels:   r.cfg.Channels,
	}
	select {
	case r.utterances <- buffer:
		return
	default:
	}

	// Keep the newest speech when downstream is slower than the microphone.
	select {
	case <-r.utterances:
		r.dropped.Add(1)
	default:
	}
	select {
	case r.utterances <- buffer:
	default:
		// A concurrent consumer may race with the replacement; boundedness wins.
		r.dropped.Add(1)
	}
}

func (r *ContinuousRecorder) publishBargeCandidate(candidate BargeCandidate) {
	if candidate.Buffer.Empty() || candidate.StartedAt.IsZero() {
		return
	}
	select {
	case r.bargeCandidates <- candidate:
		return
	default:
	}
	select {
	case <-r.bargeCandidates:
	default:
	}
	select {
	case r.bargeCandidates <- candidate:
	default:
	}
}

func (r *ContinuousRecorder) flushBargeCandidates() {
	for {
		select {
		case <-r.bargeCandidates:
		default:
			return
		}
	}
}

func (r *ContinuousRecorder) sourceFailed(process captureProcess, err error) {
	_ = process.Stop()
	r.mu.Lock()
	if r.process == process {
		r.process = nil
		r.running = false
	}
	closed := r.closed
	r.mu.Unlock()
	if !closed {
		select {
		case r.sourceErr <- err:
		default:
		}
	}
}

func (r *ContinuousRecorder) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Close terminates the persistent microphone and native detector exactly once.
func (r *ContinuousRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		process := r.process
		r.process = nil
		r.running = false
		r.mu.Unlock()

		if process != nil {
			r.closeErr = process.Stop()
		}
		r.wg.Wait()
		if err := r.vad.Close(); r.closeErr == nil {
			r.closeErr = err
		}
	})
	return r.closeErr
}

func nonNilCaptureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

var _ interface {
	Next(context.Context) (Buffer, error)
} = (*ContinuousRecorder)(nil)
