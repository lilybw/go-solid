package static

import (
	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/shared/compat"
)

// "xxxx.*", "xxx/*", "xxx.*.yyy" etc
type FileSelectorPattern = string

// DEFAULT_IGNORE are the names that are never assets: editor and VCS leavings,
// and the placeholder files that keep empty directories in git.
var DEFAULT_IGNORE = []FileSelectorPattern{
	".DS_Store", "Thumbs.db", ".gitkeep", ".gitignore", "*.map", "desktop.ini",
}

// DEFAULT_MOUNT_PATH is where the asset endpoint is mounted. Asset URLs are
// content-hashed, so everything below it is immutable and can be cached
// forever.
const DEFAULT_MOUNT_PATH = "/__go_solid_static__/"

type StaticConfig struct {
	// The path to the directory where your project has static data like images, fonts, etc.
	Location meta.AbsoluteDirectoryPath
	// Start a file watcher to have the static module update itself when the
	// contents of Location change.
	//
	// Development convenience. With it off, the module is built once at New and
	// an asset added afterwards is invisible until the next boot.
	Reactive bool
	// Ignore are glob patterns matched against each entry's base name.
	//
	// Left nil on a config you supply, it resolves to DEFAULT_IGNORE. Set it to
	// an empty slice to serve everything, including editor and VCS leavings.
	Ignore []FileSelectorPattern
	// Mux is the ServeMux, Router or the like go_solid mounts the asset
	// endpoint on. Required.
	//
	// Adapters for other router shapes live in shared/compat.
	Mux compat.MuxLike
	// MountPath is the URL prefix the endpoint answers under. Defaults to
	// DEFAULT_MOUNT_PATH. Must begin and end with "/".
	MountPath string
	// Disabled turns the feature off even when a config is supplied.
	Disabled bool
	// InlineLimit is the size in bytes below which an asset is held in memory
	// rather than read from disk per request. Left zero it resolves to
	// DEFAULT_INLINE_LIMIT. Assets above it are streamed, which is also what
	// gives range requests — and so audio and video seeking — for free.
	InlineLimit int64
}

// DEFAULT_INLINE_LIMIT is the size below which an asset is worth holding in
// memory. Icons and fonts sit under it; images and media do not.
const DEFAULT_INLINE_LIMIT = 256 << 10 // 256 KiB

// The null object describes a feature that is off, so its Ignore says nothing
// rather than defaulting: an empty slice, not nil, so a shallow copy of it can
// be appended to without reaching back here.
var NIL_STATIC_CONFIG = &StaticConfig{
	Location:    meta.AbsoluteDirectoryPath(""),
	Reactive:    false,
	Ignore:      make([]FileSelectorPattern, 0),
	Mux:         nil,
	MountPath:   DEFAULT_MOUNT_PATH,
	Disabled:    true,
	InlineLimit: DEFAULT_INLINE_LIMIT,
}

// Active reports whether the feature should be built and served.
func (cfg *StaticConfig) Active() bool {
	return cfg != nil && !cfg.Disabled && cfg.Location != ""
}
