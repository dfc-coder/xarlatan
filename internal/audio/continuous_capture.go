package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/dfc-coder/xarlatan/internal/config"
	"github.com/dfc-coder/xarlatan/internal/vad"
)

const (
	defaultContinuousPreRollChunks = 4
	defaultUtteranceQueueDepth     = 4
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

	utterances chan Buffer
	sourceErr  chan error

	mu      sync.Mutex
	process captureProcess
	running bool
	closed  bool

	wg         sync.WaitGroup
	closeOnce  sync.Once
	closeErr   error
	dropped    atomic.Uint64
	suppressed atomic.Bool
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
		cfg:        cfg,
		vad:        detector,
		preRoll:    newPreRollBuffer(options.PreRollChunks),
		factory:    factory,
		utterances: make(chan Buffer, options.QueueDepth),
		sourceErr:  make(chan error, 1),
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
	if err := r.ensureRunning(); err != nil {
		return Buffer{}, err
	}

	// Prefer already-buffered speech over a terminal source error that happened
	// after that utterance was completed.
	select {
	case buffer := <-r.utterances:
		return buffer.Clone(), nil
	default:
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

// SetSuppressed keeps the microphone process alive while discarding frames
// before VAD. WI-10D uses it during assistant playback to preserve half-duplex
// echo safety; WI-11C will replace this policy with controlled barge-in.
func (r *ContinuousRecorder) SetSuppressed(suppressed bool) {
	if r == nil {
		return
	}
	r.suppressed.Store(suppressed)
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
	wasSuppressed := r.suppressed.Load()

	for {
		n, readErr := io.ReadFull(process, buffer)
		completeBytes := n - n%(pcmBytesPerSample*r.cfg.Channels)
		if completeBytes > 0 {
			if r.suppressed.Load() {
				recording = nil
				wasSuppressed = true
			} else {
				if wasSuppressed {
					r.vad.Reset()
					r.preRoll.reset()
					wasSuppressed = false
				}
				chunk := pcmToFloat32(buffer[:completeBytes])
				var finish bool
				recording, finish = r.processChunk(recording, chunk)
				if finish {
					r.publishUtterance(recording)
					recording = nil
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
