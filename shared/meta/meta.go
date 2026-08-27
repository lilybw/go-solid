package meta

import (
	"fmt"
	"regexp"
	"strings"
)

// The vocabulary is aliases rather than defined types: it documents what a
// string means without forcing a conversion at every boundary.
type (
	// QualifiedName is a component's registry key: the path relative to the
	// components root, without extension, optionally suffixed "#<Export>".
	QualifiedName = string
	// ExportName is the export selected out of a component file. Empty means
	// the default export.
	ExportName = string

	AbsoluteFilePath      = string
	AbsoluteDirectoryPath = string
	RelativeFilePath      = string
	RelativeDirectoryPath = string

	// FileExtension includes the leading dot, as filepath.Ext returns it.
	FileExtension = string
	// FileSelectorPattern is a glob matched against a base name, as
	// filepath.Match takes it: "*.map", "Thumbs.db".
	FileSelectorPattern = string
	// ModuleSpecifier is an import specifier as written in source, relative
	// ("./Button") or bare ("solid-js").
	ModuleSpecifier = string

	// JSIdentifier is a name that may appear in generated JavaScript.
	JSIdentifier = string
	// TSTypeExpression is a TypeScript type as written, e.g. "string | null".
	TSTypeExpression = string

	// ContentDigest is a file's content hash, "sha256:<hex>".
	ContentDigest = string
	// Fingerprint abstractly represents the conditions of something's
	// creation, such as the bundler settings an artifact was built under.
	Fingerprint = string
	// CacheKeyString is a CacheKey rendered as the stem its entry is filed under.
	CacheKeyString = string

	// URLPath is a path within the served origin, always leading with "/".
	URLPath = string
	// MIMEType is a media type without parameters, e.g. "image/svg+xml".
	MIMEType = string

	HTMLElementID = string
	HTMLTagName   = string

	Void any
)

// void
var VOID Void = nil

func Zero[T any]() T {
	var t T
	return t
}

type Names[E ~uint8 | ~uint | ~int] map[E]string

func (n Names[E]) Of(typeName string, value E) string {
	if name, ok := n[value]; ok {
		return name
	}
	return fmt.Sprintf("%s(%d)", typeName, value)
}

// Copy returns a heap-allocated shallow copy of src.
func Copy[T any](src *T) *T {
	c := *src
	return &c
}

var NIL_PROPS = Zero[any]()

// EXPORT_SELECTOR separates a component file from the export to take out of it.
const EXPORT_SELECTOR = "#"

// DEFAULT_EXPORT is ESM's name for the default export.
const DEFAULT_EXPORT = "default"

// SplitSelector separates a qualified name into the file it names and the
// export to take from it. An empty export means the default one.
func SplitSelector(name QualifiedName) (file QualifiedName, export string) {
	i := strings.LastIndex(name, EXPORT_SELECTOR)
	if i < 0 {
		return name, ""
	}
	file, export = name[:i], name[i+len(EXPORT_SELECTOR):]
	if export == DEFAULT_EXPORT {
		export = ""
	}
	return file, export
}

// JoinSelector is the inverse of SplitSelector.
func JoinSelector(file QualifiedName, export string) QualifiedName {
	if export == "" || export == DEFAULT_EXPORT {
		return file
	}
	return file + EXPORT_SELECTOR + export
}

// jsIdentifier matches the export names that can appear in an import clause.
// A selector naming anything else could not be imported, and would otherwise
// be pasted straight into generated JavaScript.
var jsIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// ValidExportName reports whether export can be imported by name. The empty
// string (the default export) is always valid.
func ValidExportName(export string) bool {
	return export == "" || jsIdentifier.MatchString(export)
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

func Or[T comparable](v, def T) T {
	return Ternary(v == Zero[T](), def, v)
}
