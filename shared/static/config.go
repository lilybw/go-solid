package static

import "github.com/lilybw/go-solid/internal/meta"

// "xxxx.*", "xxx/*", "xxx.*.yyy" etc
type FileSelectorPattern = string

type StaticConfig struct {
	// The path to the directory where your project has static data like images, fonts, etc.
	Location meta.AbsoluteDirectoryPath
	// Start a file watcher to have the StaticRegistry update itself when contents of StaticConfig.Location change.
	Reactive bool
	Ignore   []FileSelectorPattern
}

var NIL_STATIC_CONFIG = &StaticConfig{
	Location: meta.AbsoluteDirectoryPath(""),
	Reactive: false,
}
