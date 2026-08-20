package meta

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

type TBD any

var NIL_PROPS = Zero[any]()

// Rendered is the cacheable artifact set for one component+props combination.
type Rendered struct {
	HTML    string // index.html referencing the CSS + JS
	CSS     string
	JS      string
	CSSName string // predictable filename, e.g. "auth_LoginForm.<hash>.css"
	JSName  string
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
