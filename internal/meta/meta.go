package meta

import (
	"regexp"
	"strings"
)

// Name of component with path from registry directory, however without extension.
type QualifiedName = string

type AbsoluteFilePath = string

type AbsoluteDirectoryPath = string

type RelativeFilePath = string

type RelativeDirectoryPath = string

// void
type Void any

// void
var VOID Void = nil

func Zero[T any]() T {
	var t T
	return t
}

// Copy returns a heap-allocated shallow copy of src.
//
// Intended for null-object singletons: a Config must never alias one, or
// normalization writes through to package-level state shared by every Config in
// the process.
//
//	cfg.HMR = meta.Copy(hmr.NIL_HMR_CONFIG)
//	cfg.HMR.Disabled = false // NIL_HMR_CONFIG is untouched
//
// The copy is shallow. Slice, map and pointer fields still share storage with
// src and must be cloned separately if they will be mutated.
func Copy[T any](src *T) *T {
	c := *src
	return &c
}

var NIL_PROPS = Zero[any]()

// EXPORT_SELECTOR separates a component file from the export to take out of it.
//
//	"auth/LoginForm"          the file's default export
//	"auth/LoginForm#Submit"   the file's exported Submit
const EXPORT_SELECTOR = "#"

// DEFAULT_EXPORT is ESM's name for the default export. Accepted as an explicit
// spelling — "auth/LoginForm#default" and "auth/LoginForm" select the same
// thing — so a generator emitting selectors need not special-case it.
const DEFAULT_EXPORT = "default"

// SplitSelector separates a qualified name into the file it names and the
// export to take from it. An empty export means the default one.
//
//	file, export := meta.SplitSelector("auth/LoginForm#Submit") // "auth/LoginForm", "Submit"
//	file, export := meta.SplitSelector("auth/LoginForm")        // "auth/LoginForm", ""
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
