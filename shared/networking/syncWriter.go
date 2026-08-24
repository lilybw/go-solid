package networking

import (
	"net/http"
	"sync"
)

// SynchronizedResponseWriter serializes access to an http.ResponseWriter.
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

func (this *SynchronizedResponseWriter) Unwrap() http.ResponseWriter { return this.w }
