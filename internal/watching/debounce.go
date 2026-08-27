package watching

import (
	"sync"
	"time"
)

// Debouncer collects rapid events into one call of fn, made after the window
// elapses with no further arming. It is safe for concurrent use.
//
//	d := watching.NewDebouncer(120*time.Millisecond, rebuild)
//	defer d.Stop() // waits for a fire already in progress
type Debouncer struct {
	window time.Duration
	fn     func()

	mu      sync.Mutex
	timer   *time.Timer
	stopped bool
	running sync.WaitGroup
}

func NewDebouncer(window time.Duration, fn func()) *Debouncer {
	return &Debouncer{window: window, fn: fn}
}

// Arm restarts the window. A Debouncer that has been stopped ignores this, so
// a fire cannot outlive the resources it would touch.
func (d *Debouncer) Arm() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.window, d.fire)
}

func (d *Debouncer) fire() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.running.Add(1)
	d.mu.Unlock()
	defer d.running.Done()

	d.fn()
}

// Stop prevents further fires and waits for one already in progress. It is
// idempotent and safe on a nil receiver.
func (d *Debouncer) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Unlock()

	d.running.Wait()
}
