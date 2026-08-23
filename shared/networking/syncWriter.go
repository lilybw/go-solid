package networking

import (
	"net/http"
	"sync"
)

// SynchronizedResponseWriter serializes access to an http.ResponseWriter.
//
// Chains registered under one event dispatch concurrently (see
// HANDLER_MODE_PARALLEL) and http.ResponseWriter is not safe for concurrent
// use: two handlers writing at once can interleave mid-buffer or race on the
// header map. Wrapping the writer once, where it is bound, makes every handler
// queue behind whoever holds it.
//
// Safe is not the same as ordered. Which handler's bytes land first is still
// whichever goroutine wins the lock, so two handlers that each write a body
// still produce one of two documents — just never a torn one. Handlers whose
// order matters belong in one chain, where they run in sequence.
//
// Header returns the wrapped writer's map, and writes through that map are not
// covered by the lock. Set headers before dispatch, or from a single chain.
type SynchronizedResponseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
}

// Synchronized wraps w, unless it is nil or already wrapped.
func Synchronized(w http.ResponseWriter) http.ResponseWriter {
	switch w.(type) {
	case nil:
		return nil
	case *SynchronizedResponseWriter:
		return w
	}
	return &SynchronizedResponseWriter{w: w}
}

func (this *SynchronizedResponseWriter) Header() http.Header {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.w.Header()
}

func (this *SynchronizedResponseWriter) Write(b []byte) (int, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.w.Write(b)
}

func (this *SynchronizedResponseWriter) WriteHeader(code int) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.w.WriteHeader(code)
}

// Flush forwards to the wrapped writer when it supports flushing.
func (this *SynchronizedResponseWriter) Flush() {
	this.mu.Lock()
	defer this.mu.Unlock()
	if flusher, ok := this.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap exposes the wrapped writer, which is how http.ResponseController
// reaches capabilities this type does not forward itself.
func (this *SynchronizedResponseWriter) Unwrap() http.ResponseWriter { return this.w }
