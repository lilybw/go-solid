package meta

import (
	"strings"
	"sync"
)

// Name of component with path from registry directory, however without extension.
type QualifiedName = string

type AbsoluteFilePath = string

type AbsoluteDirectoryPath = string

type RelativeFilePath = string

type RelativeDirectoryPath = string

// Rendered is the cacheable artifact set for one component+props combination.
type Rendered struct {
	HTML    string // index.html referencing the CSS + JS
	CSS     string
	JS      string
	CSSName string // predictable filename, e.g. "auth_LoginForm.<hash>.css"
	JSName  string
}

// RingBuffer keeps the last max bytes written to it, concurrency-safe. Captures
// worker stderr so that when a worker dies it can report WHY (a Node stack
// trace, ERR_MODULE_NOT_FOUND, etc.) instead of a bare "EOF".
type RingBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func NewRingBuffer(max int) *RingBuffer { return &RingBuffer{max: max} }

func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *RingBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(string(r.buf))
}

func PanicIfTrue(pred bool, msg string) {
	if pred {
		panic(msg)
	}
}

func Ternary[T any](pred bool, a, b T) T {
	if pred {
		return a
	}
	return b
}

type BuilderLike any

// Encapsulated configuration call. Requires BuilderLike to have a fluid interface (i.e. return self on each method call)
type Configurator[T BuilderLike] = func(T)
