// Package sources resolves a component's text, wherever that component lives.
//
// It is the single place go_solid learns what a component says: type
// extraction, server rendering and entry generation all read through a Reader.
// A bundler with no filesystem is therefore a bundler with a different Reader,
// and nothing downstream of one has to know which it got.
package sources

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/lilybw/go-solid/internal/hashing"
	"github.com/lilybw/go-solid/shared/meta"
)

// Stamp is a source's cheap identity: equal stamps mean equal text, so a stamp
// that still holds spares a reader the read.
type Stamp = string

// Source is one component file as a reader answers for it.
type Source struct {
	Text  string
	Stamp Stamp
	// Inline reports that nothing can resolve an import of this source, so
	// whatever depends on it has to carry its text instead.
	Inline bool
}

// Reader answers for the component files it holds.
type Reader interface {
	// Read returns the source at path.
	Read(path meta.AbsoluteFilePath) (Source, error)
	// Stamp identifies the text at path without reading it.
	Stamp(path meta.AbsoluteFilePath) (Stamp, error)
	// Digest is the content hash a cache records a source under.
	Digest(path meta.AbsoluteFilePath) (meta.ContentDigest, bool)
}

// -----------------------------------------------------------------------------
// Disk
// -----------------------------------------------------------------------------

type disk struct{}

// Disk reads component files from the filesystem.
func Disk() Reader { return disk{} }

// OrDisk resolves a reader that was never supplied. The filesystem is what
// everything but an ephemeral bundler wants, so leaving it out asks for it.
func OrDisk(reader Reader) Reader {
	if reader == nil {
		return Disk()
	}
	return reader
}

func (disk) Stamp(path meta.AbsoluteFilePath) (Stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// Concatenated rather than formatted: a stamp is compared, never read, and
	// this is on the path every prepared render takes.
	return strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10), nil
}

func (d disk) Read(path meta.AbsoluteFilePath) (Source, error) {
	// Stamped before the read, so a stamp never describes text older than the
	// text it was stored beside.
	stamp, err := d.Stamp(path)
	if err != nil {
		return Source{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	return Source{Text: string(raw), Stamp: stamp}, nil
}

func (disk) Digest(path meta.AbsoluteFilePath) (meta.ContentDigest, bool) {
	return hashing.OfFile(path)
}

// -----------------------------------------------------------------------------
// Memory
// -----------------------------------------------------------------------------

// MEMORY_ROOT prefixes the paths of sources that exist only in this process. It
// is not a location: it is a name no file answers to.
const MEMORY_ROOT = "/__go_solid_memory__"

// MemoryPath names an in-process source. The extension is kept, since it is what
// selects the dialect everything downstream parses it in.
//
//	MemoryPath("anonymous/Anon_0f2c1a.tsx")
func MemoryPath(name meta.RelativeFilePath) meta.AbsoluteFilePath {
	return MEMORY_ROOT + "/" + strings.TrimPrefix(name, "/")
}

// Memory holds sources that were never written down. It is content-addressed by
// convention rather than by rule: a path put twice with different text takes the
// later text, and every stamp derived from it changes with it.
type Memory struct {
	mu   sync.RWMutex
	held map[meta.AbsoluteFilePath]Source
}

func NewMemory() *Memory {
	return &Memory{held: make(map[meta.AbsoluteFilePath]Source)}
}

// Put holds text under path, replacing whatever was there.
func (m *Memory) Put(path meta.AbsoluteFilePath, text string) {
	source := Source{Text: text, Stamp: hashing.OfBytes([]byte(text)), Inline: true}
	m.mu.Lock()
	m.held[path] = source
	m.mu.Unlock()
}

func (m *Memory) Read(path meta.AbsoluteFilePath) (Source, error) {
	m.mu.RLock()
	source, ok := m.held[path]
	m.mu.RUnlock()
	if !ok {
		return Source{}, notHeld(path)
	}
	return source, nil
}

func (m *Memory) Stamp(path meta.AbsoluteFilePath) (Stamp, error) {
	source, err := m.Read(path)
	return source.Stamp, err
}

// Digest is the stamp: an in-process source is identified by its content and by
// nothing else.
func (m *Memory) Digest(path meta.AbsoluteFilePath) (meta.ContentDigest, bool) {
	source, err := m.Read(path)
	return source.Stamp, err == nil
}

func notHeld(path meta.AbsoluteFilePath) error {
	return fmt.Errorf("go_solid/sources: nothing held at %q", path)
}

// -----------------------------------------------------------------------------
// Overlay
// -----------------------------------------------------------------------------

// Overlay answers from the first reader that holds a path. It is what lets a
// disk-backed bundler serve components that were never written down.
//
//	sources.Overlay{memory, sources.Disk()}
type Overlay []Reader

func (o Overlay) Read(path meta.AbsoluteFilePath) (Source, error) {
	var err error
	for _, reader := range o {
		var source Source
		if source, err = reader.Read(path); err == nil {
			return source, nil
		}
	}
	return Source{}, o.reason(path, err)
}

func (o Overlay) Stamp(path meta.AbsoluteFilePath) (Stamp, error) {
	var err error
	for _, reader := range o {
		var stamp Stamp
		if stamp, err = reader.Stamp(path); err == nil {
			return stamp, nil
		}
	}
	return "", o.reason(path, err)
}

func (o Overlay) Digest(path meta.AbsoluteFilePath) (meta.ContentDigest, bool) {
	for _, reader := range o {
		if digest, ok := reader.Digest(path); ok {
			return digest, true
		}
	}
	return "", false
}

// reason keeps the last layer's error, which is the one that describes a path
// that was meant to exist.
func (o Overlay) reason(path meta.AbsoluteFilePath, err error) error {
	if err == nil {
		return notHeld(path)
	}
	return err
}
