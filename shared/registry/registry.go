package registry

import (
	"fmt"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
)

// Component is one entry discovered in the components directory.
type Component struct {
	// Name is the registry key: the path relative to the components root,
	// without extension, using forward slashes. e.g. "auth/LoginForm".
	Name meta.QualifiedName
	// Path is the absolute path to the .tsx/.jsx source file on disk.
	Path meta.AbsoluteFilePath
	// Ext is the source extension (".tsx", ".jsx", ".ts", ".js").
	Ext         string
	MountRootID string // optional: if non-empty, the HTML shell will mount this component at this id instead of the default "go-solid-root"
}

func NewComponent(name meta.QualifiedName, path meta.AbsoluteFilePath, ext string) Component {
	return Component{Name: name, Path: path, Ext: ext, MountRootID: fmt.Sprintf("%s-go-solid-root", strings.ReplaceAll(name, "/", "-"))}
}
