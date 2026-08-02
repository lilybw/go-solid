package static

import "github.com/lilybw/go-solid/internal/meta"

type StaticConfig struct {
	// The path to the directory where your project has static data like images, fonts, etc.
	Location meta.AbsoluteDirectoryPath
	// Start a file watcher to have the StaticRegistry update itself when contents of StaticConfig.Location change.
	Reactive bool
}

var NIL_STATIC_CONFIG = &StaticConfig{
	Location: meta.AbsoluteDirectoryPath(""),
	Reactive: false,
}
