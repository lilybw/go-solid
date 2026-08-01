package workers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lilybw/go-solid/internal/meta"
)

// TransformRequest / transformResponse mirror the NDJSON protocol spoken by
// internal/worker/transform-worker.mjs.
type TransformRequest struct {
	ID         int64  `json:"id"`
	Filename   string `json:"filename"`
	Code       string `json:"code"`
	Generate   string `json:"generate"` // "dom" | "ssr"
	Hydratable bool   `json:"hydratable"`
}

type TransformResponse struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Code  string `json:"code"`
	Error string `json:"error"`
}

// Worker is a single long-lived Node process.
type Worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	dec      *bufio.Reader
	enc      *json.Encoder
	errBuf   *meta.RingBuffer // last bytes the worker wrote to stderr
	dead     atomic.Bool      // set once the process is known unusable
	killOnce sync.Once        // ensures teardown (incl. a single cmd.Wait) runs once
}

// Pool is a set of transform workers. Size may be 1 (default) or more; the
// interface is identical either way, so concurrency can be tuned later without
// touching call sites. Dead workers are replaced on demand.
type Pool struct {
	workers chan *Worker
	nextID  atomic.Int64
	nodeBin string
	script  meta.AbsoluteFilePath
	deps    meta.AbsoluteDirectoryPath
	timeout time.Duration
	closed  atomic.Bool
}

// PoolConfig configures worker startup.
type PoolConfig struct {
	Size         int                        // number of Node processes; <=0 means 1
	NodeBin      string                     // path to node; "" means "node" on PATH
	Script       meta.AbsoluteFilePath      // absolute path to transform-worker.mjs
	Dependencies meta.AbsoluteDirectoryPath // cwd for workers (must resolve babel-preset-solid)
	Timeout      time.Duration              // per-transform timeout; 0 means 30s
}

func NewPool(cfg PoolConfig) (*Pool, error) {
	size := cfg.Size
	if size <= 0 {
		size = 1
	}
	node := cfg.NodeBin
	if node == "" {
		node = "node"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	p := &Pool{
		workers: make(chan *Worker, size),
		nodeBin: node,
		script:  cfg.Script,
		deps:    cfg.Dependencies,
		timeout: timeout,
	}

	for i := 0; i < size; i++ {
		w, err := p.spawn()
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("pool: spawn worker %d: %w", i, err)
		}
		p.workers <- w
	}
	return p, nil
}

func (p *Pool) spawn() (*Worker, error) {
	cmd := exec.Command(p.nodeBin, p.script, p.deps)
	cmd.Dir = p.deps

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	errBuf := meta.NewRingBuffer(8 << 10) // last 8KB
	go io.Copy(errBuf, stderr)            //nolint:errcheck // best-effort diagnostics

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start node (%s %s): %w", p.nodeBin, p.script, err)
	}

	w := &Worker{
		cmd:    cmd,
		stdin:  stdin,
		dec:    bufio.NewReaderSize(stdout, 1<<20),
		enc:    json.NewEncoder(stdin),
		errBuf: errBuf,
	}

	// Fail fast: probe with a trivial transform so a broken environment (missing
	// babel, ESM resolution failure, wrong Node) surfaces at construction with
	// the actual stderr, not later as a bare EOF.
	if err := w.probe(); err != nil {
		w.kill()
		stderrTail := w.errBuf.String()
		if stderrTail != "" {
			return nil, fmt.Errorf("worker failed to start: %w\nworker stderr:\n%s", err, stderrTail)
		}
		return nil, fmt.Errorf("worker failed to start: %w", err)
	}

	return w, nil
}

// probe sends one no-op transform and waits for a valid response.
func (w *Worker) probe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := w.roundtrip(ctx, TransformRequest{
		Filename: "__probe__.jsx",
		Code:     "export default 0;",
		Generate: "dom",
	})
	return err
}

// kill terminates the worker process and marks it dead. Safe to call from
// multiple goroutines and multiple times; teardown runs exactly once.
func (w *Worker) kill() {
	w.dead.Store(true)
	w.killOnce.Do(func() {
		_ = w.stdin.Close()
		if w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
		// Reap exactly once so the OS doesn't keep a zombie. cmd.Wait must never
		// be called concurrently with itself, which is why this is inside Once.
		go func() { _ = w.cmd.Wait() }()
	})
}

// Transform runs one JSX->Solid transform on a free worker. If the chosen worker
// has died, it is replaced and the request retried once on a fresh worker, so a
// single crash does not poison the pool.
func (p *Pool) Transform(ctx context.Context, req TransformRequest) (string, error) {
	if p.closed.Load() {
		return "", fmt.Errorf("pool: closed")
	}

	// One retry: a worker can die between checkout and use.
	for attempt := 0; attempt < 2; attempt++ {
		var w *Worker
		select {
		case w = <-p.workers:
		case <-ctx.Done():
			return "", ctx.Err()
		}

		// If this worker is known-dead (killed by a previous timeout), replace it
		// before returning it to rotation, and try again.
		if w.dead.Load() {
			p.replace(w)
			continue
		}

		out, err := p.run(ctx, w, req)

		if w.dead.Load() {
			// The worker died during this request (timeout kill or pipe EOF).
			// Replace it rather than returning a corpse to the channel.
			p.replace(w)
			// Retry once on a fresh worker for transient death; surface the error
			// on the second failure.
			if attempt == 0 && !p.closed.Load() && ctx.Err() == nil {
				continue
			}
			return out, err
		}

		// Healthy worker: return it to rotation.
		p.workers <- w
		return out, err
	}
	return "", fmt.Errorf("pool: transform failed after retry")
}

// run executes one round-trip with the pool timeout, marking the worker dead on
// any failure that indicates the process is no longer usable.
func (p *Pool) run(ctx context.Context, w *Worker, req TransformRequest) (string, error) {
	req.ID = p.nextID.Add(1)

	tctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	out, err := w.roundtrip(tctx, req)
	if err != nil {
		// A protocol/IO error or a kill means the process is unusable. A *babel*
		// error (resp.OK == false) is a clean, expected failure and does NOT
		// kill the worker — roundtrip distinguishes these via errTransform.
		if !isTransformError(err) {
			w.kill()
		}
	}
	return out, err
}

// replace spawns a fresh worker to keep the pool at capacity. If respawn fails
// (e.g. environment now broken), the slot is left empty rather than filled with
// a corpse; the pool degrades in capacity but never hands out dead workers. A
// pool that loses all workers will return spawn errors on the next call.
func (p *Pool) replace(dead *Worker) {
	dead.kill()
	if p.closed.Load() {
		return
	}
	w, err := p.spawn()
	if err != nil {
		// Can't respawn right now; don't block. The channel simply has one fewer
		// worker. Surfacing this is the caller's next-call spawn error.
		return
	}
	p.workers <- w
}

// errTransform marks a clean, expected transform failure (bad user JSX) as
// opposed to a dead-process error. Such workers stay alive.
type errTransform struct{ msg string }

func (e *errTransform) Error() string { return e.msg }

func isTransformError(err error) bool {
	_, ok := err.(*errTransform)
	return ok
}

// roundtrip writes one request and reads exactly one response line. The worker
// processes one request at a time (guaranteed by the pool channel handing out
// each worker to a single caller), so lines can't interleave.
func (w *Worker) roundtrip(ctx context.Context, req TransformRequest) (string, error) {
	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)

	go func() {
		if err := w.enc.Encode(&req); err != nil { // Encode appends '\n'
			done <- result{err: fmt.Errorf("write request: %w", err)}
			return
		}
		line, err := w.dec.ReadBytes('\n')
		if err != nil {
			done <- result{err: fmt.Errorf("read response: %w", err)}
			return
		}
		var resp TransformResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			done <- result{err: fmt.Errorf("decode response: %w", err)}
			return
		}
		if resp.ID != req.ID {
			done <- result{err: fmt.Errorf("response id mismatch: got %d want %d", resp.ID, req.ID)}
			return
		}
		if !resp.OK {
			// Clean transform failure (bad JSX). Worker is still healthy.
			done <- result{err: &errTransform{msg: "transform failed: " + resp.Error}}
			return
		}
		done <- result{code: resp.Code}
	}()

	select {
	case r := <-done:
		return r.code, r.err
	case <-ctx.Done():
		// The worker may be wedged on this request; kill it so the pool replaces
		// it rather than reusing a desynced process.
		w.kill()
		return "", ctx.Err()
	}
}

// Close terminates all workers and prevents further use.
func (p *Pool) Close() {
	if p.closed.Swap(true) {
		return
	}
	// Drain whatever workers are currently in the channel and kill them.
	for {
		select {
		case w := <-p.workers:
			if w != nil {
				w.kill()
			}
		default:
			return
		}
	}
}
