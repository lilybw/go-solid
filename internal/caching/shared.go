package caching

import (
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
}

const CACHE_DIR_NAME = "component_cache"

// componentIsInFile reports whether a component selector is backed by the given
// component file: the file itself, or an export selected out of it.
func componentIsInFile(component, file meta.QualifiedName) bool {
	return component == file || strings.HasPrefix(component, file+meta.EXPORT_SELECTOR)
}
