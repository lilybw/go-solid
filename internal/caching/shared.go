package caching

import (
	"path/filepath"
	"strings"

	"github.com/lilybw/go-solid/shared/meta"
)

// Rendered is the cacheable artifact set for one component+props combination.
type Rendered struct {
	HTML    string // index.html referencing the CSS + JS
	CSS     string
	JS      string
	CSSName string // predictable filename, e.g. "auth_LoginForm.<hash>.css"
	JSName  string
	// OPTIONAL, not necessarily populated, not necessarily present, never to be included in disk nor mem caching nor any cache keys
	DataIslands map[meta.HTMLElementID]meta.JSONString `json:"-"`
}

const CACHE_DIR_NAME = "component_cache"

// componentIsInFile reports whether a component selector is backed by the given
// component file: the file itself, or an export selected out of it.
func componentIsInFile(component, file meta.QualifiedName) bool {
	return component == file || strings.HasPrefix(component, file+meta.EXPORT_SELECTOR)
}

// SafeStem flattens a selector into a filename. The export separator folds with
// the path separators: a stem ends up in filenames and in the asset names those
// filenames are served under, and "#" does not survive a URL.
func SafeStem(component meta.QualifiedName) string {
	return strings.NewReplacer(
		"/", "_", string(filepath.Separator), "_", meta.EXPORT_SELECTOR, "_",
	).Replace(component)
}
