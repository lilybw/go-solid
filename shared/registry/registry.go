package registry

import (
	"fmt"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
)

type Component struct {
	// Name is the registry key: the path relative to the components root,
	// without extension, using forward slashes, optionally followed by
	// "#<Export>". e.g. "auth/LoginForm" or "auth/LoginForm#Submit".
	Name meta.QualifiedName
	// Path is the absolute path to the .tsx/.jsx source file on disk.
	Path meta.AbsoluteFilePath
	// Ext is the source extension (".tsx", ".jsx", ".ts", ".js").
	Ext string
	// Export is the name to take out of the file. Empty means the default
	// export, which is the only thing an unsuffixed selector ever names.
	Export      string
	MountRootID string // the id of the element the HTML shell mounts this component on
}

func NewComponent(name meta.QualifiedName, path meta.AbsoluteFilePath, ext string) *Component {
	return &Component{Name: name, Path: path, Ext: ext, MountRootID: MountRootIDFor(name)}
}

// WithExport returns the sibling component backed by the same file: the named
// export rather than whatever this one selects.
func (this *Component) WithExport(export string) *Component {
	file, _ := meta.SplitSelector(this.Name)
	name := meta.JoinSelector(file, export)
	return &Component{
		Name:        name,
		Path:        this.Path,
		Ext:         this.Ext,
		Export:      export,
		MountRootID: MountRootIDFor(name),
	}
}

// MountRootIDFor derives the shell's mount element id from a selector. Both
// separators fold to "-", so the id stays a usable one and two exports of the
// same file never mount on the same element.
func MountRootIDFor(name meta.QualifiedName) string {
	flat := strings.NewReplacer("/", "-", meta.EXPORT_SELECTOR, "-").Replace(name)
	return fmt.Sprintf("%s-go-solid-root", flat)
}
