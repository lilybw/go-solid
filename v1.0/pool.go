package go_solid

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
)

// transformRequest / transformResponse mirror the NDJSON protocol spoken by
// internal/worker/transform-worker.mjs.
type transformRequest struct {
	ID         int64  `json:"id"`
	Filename   string `json:"filename"`
	Code       string `json:"code"`
	Generate   string `json:"generate"` // "dom" | "ssr"
	Hydratable bool   `json:"hydratable"`
}

type transformResponse struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Code  string `json:"code"`
	Error string `json:"error"`
}

// worker is a single long-lived Node process.
type worker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex // serializes writes; one in-flight request per worker
	dec    *bufio.Reader
	enc    *json.Encoder
	closed atomic.Bool
}

// Pool is a set of transform workers. Size may be 1 (default) or more; the
// interface is identical either way, so concurrency can be tuned later without
// touching call sites.
type Pool struct {
	workers chan *worker
	all     []*worker
	nextID  atomic.Int64
	nodeBin string
	script  string
	timeout time.Duration
	closed  atomic.Bool
}

// PoolConfig configures worker startup.
type PoolConfig struct {
	Size       int           // number of Node processes; <=0 means 1
	NodeBin    string        // path to node; "" means "node" on PATH
	ScriptPath string        // absolute path to transform-worker.mjs
	WorkDir    string        // cwd for workers (must resolve babel-preset-solid)
	Timeout    time.Duration // per-transform timeout; 0 means 30s
}

func newPool(cfg PoolConfig) (*Pool, error) {
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
		workers: make(chan *worker, size),
		nodeBin: node,
		script:  cfg.ScriptPath,
		timeout: timeout,
	}

	for i := 0; i < size; i++ {
		w, err := p.spawn(cfg.WorkDir)
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("pool: spawn worker %d: %w", i, err)
		}
		p.all = append(p.all, w)
		p.workers <- w
	}
	return p, nil
}

func (p *Pool) spawn(workDir string) (*worker, error) {
	cmd := exec.Command(p.nodeBin, p.script)
	cmd.Dir = workDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Drain stderr so a chatty worker can't block on a full pipe. In production
	// you'd route this to your logger; here we discard.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	go io.Copy(io.Discard, stderr)

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	w := &worker{
		cmd:   cmd,
		stdin: stdin,
		dec:   bufio.NewReaderSize(stdout, 1<<20),
		enc:   json.NewEncoder(stdin),
	}
	return w, nil
}

// Transform runs one JSX->Solid transform on any free worker. Blocks until a
// worker is available or ctx is cancelled.
func (p *Pool) Transform(ctx context.Context, req transformRequest) (string, error) {
	if p.closed.Load() {
		return "", fmt.Errorf("pool: closed")
	}

	var w *worker
	select {
	case w = <-p.workers:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { p.workers <- w }()

	req.ID = p.nextID.Add(1)

	// Bound the transform with the pool timeout unless ctx is tighter.
	tctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	return w.roundtrip(tctx, req)
}

// roundtrip writes one request and reads exactly one response line. A worker
// processes one request at a time (guaranteed by the pool channel), so lines
// can't interleave; we still verify the id matches.
func (w *worker) roundtrip(ctx context.Context, req transformRequest) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

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
		var resp transformResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			done <- result{err: fmt.Errorf("decode response: %w", err)}
			return
		}
		if resp.ID != req.ID {
			done <- result{err: fmt.Errorf("response id mismatch: got %d want %d", resp.ID, req.ID)}
			return
		}
		if !resp.OK {
			done <- result{err: fmt.Errorf("transform failed: %s", resp.Error)}
			return
		}
		done <- result{code: resp.Code}
	}()

	select {
	case r := <-done:
		return r.code, r.err
	case <-ctx.Done():
		// The worker may be wedged on this request; kill it so the pool doesn't
		// hand out a desynced process. A supervisor could respawn; kept simple here.
		w.closed.Store(true)
		_ = w.cmd.Process.Kill()
		return "", ctx.Err()
	}
}

// Close terminates all workers.
func (p *Pool) Close() {
	if p.closed.Swap(true) {
		return
	}
	for _, w := range p.all {
		if w == nil {
			continue
		}
		_ = w.stdin.Close()
		if w.cmd.Process != nil {
			_ = w.cmd.Process.Kill()
		}
	}
}
