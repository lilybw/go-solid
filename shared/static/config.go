package static

import (
	"strings"

	"github.com/lilybw/go-solid/shared/compat"
	"github.com/lilybw/go-solid/shared/meta"
)

// "xxxx.*", "xxx/*", "xxx.*.yyy" etc
type FileSelectorPattern = string

// DEFAULT_IGNORE are the names that are never assets: editor and VCS leavings,
// and the placeholder files that keep empty directories in git.
var DEFAULT_IGNORE = []FileSelectorPattern{
	".DS_Store", "Thumbs.db", ".gitkeep", ".gitignore", "*.map", "desktop.ini",
}

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
	Mux compat.MuxLike `json:"-"`
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

var NIL_STATIC_CONFIG = &StaticConfig{
	Location:    meta.AbsoluteDirectoryPath(""),
	Reactive:    false,
	Ignore:      make([]FileSelectorPattern, 0),
	Mux:         nil,
	MountPath:   DEFAULT_MOUNT_PATH,
	Disabled:    true,
	InlineLimit: DEFAULT_INLINE_LIMIT,
}

func (cfg *StaticConfig) Active() bool {
	return cfg != nil && !cfg.Disabled && cfg.Location != ""
}

// The Effective* accessors resolve a setting to what will actually be used.
func (cfg *StaticConfig) EffectiveMountPath() string {
	path := DEFAULT_MOUNT_PATH
	if cfg != nil && cfg.MountPath != "" {
		path = cfg.MountPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func (cfg *StaticConfig) EffectiveIgnore() []FileSelectorPattern {
	if cfg == nil || cfg.Ignore == nil {
		return DEFAULT_IGNORE
	}
	return cfg.Ignore
}

func (cfg *StaticConfig) EffectiveInlineLimit() int64 {
	if cfg == nil || cfg.InlineLimit == 0 {
		return DEFAULT_INLINE_LIMIT
	}
	return cfg.InlineLimit
}
