package types

import "fmt"

// CheckMode selects when go_solid holds the Go props handed to a template
// against the TypeScript type the component declares for them.
//
// The component is the contract. Correlation is covariant, in the sense of
// Java's <? extends T>: props satisfy a component when they carry every field
// it requires, at a compatible type. Supplying extra fields, or declaring them
// in another order, is not a finding.
//
// A mismatch fails the pass that found it: at boot it fails New, at runtime it
// fails that render. Extracting and caching the component's props type is not
// gated by this setting.
type CheckMode uint8

const (
	// CHECK_UNSET is the zero value: a config that never named a mode.
	// Normalization resolves it to DEFAULT_CHECK.
	CHECK_UNSET CheckMode = iota

	// CHECK_BOOT correlates every component's declared props type with its
	// generated definition once, during New.
	//
	// The pass reads and parses component sources; it neither bundles nor
	// renders, so it is independent of rasterization and of Generation.
	CHECK_BOOT

	// CHECK_RUNTIME holds the Go props against the component's props type on
	// every Bundler#Prepare.
	CHECK_RUNTIME

	// CHECK_RUNTIME_AND_BOOT runs both passes. CHECK_UNSET resolves to this.
	CHECK_RUNTIME_AND_BOOT

	// CHECK_NEVER reports nothing. Props types are still extracted and cached.
	CHECK_NEVER
)

// DEFAULT_CHECK is what CHECK_UNSET resolves to.
const DEFAULT_CHECK = CHECK_RUNTIME_AND_BOOT

// TYPES_DIR_NAME is the sub-directory of Config#Workspace holding the
// definitions a component may import, for props go_solid synthesises rather
// than the consumer writing them — routes, static assets, and so on.
//
// Nothing derived from a component is written here: a component already states
// its own props type, and that statement is what go_solid checks against. The
// shapes it extracts are an internal cache kept elsewhere in the workspace.
const TYPES_DIR_NAME = "types"

func (c CheckMode) String() string {
	switch c {
	case CHECK_UNSET:
		return "UNSET"
	case CHECK_BOOT:
		return "BOOT"
	case CHECK_RUNTIME:
		return "RUNTIME"
	case CHECK_RUNTIME_AND_BOOT:
		return "RUNTIME_AND_BOOT"
	case CHECK_NEVER:
		return "NEVER"
	default:
		return fmt.Sprintf("CheckMode(%d)", uint8(c))
	}
}

// MarshalJSON renders the mode by name, so a logged config reads
// "RUNTIME_AND_BOOT" rather than 3.
func (c CheckMode) MarshalJSON() ([]byte, error) {
	return []byte(`"` + c.String() + `"`), nil
}

// AtBoot reports whether the boot pass runs.
func (c CheckMode) AtBoot() bool {
	return c == CHECK_BOOT || c == CHECK_RUNTIME_AND_BOOT
}

// AtRuntime reports whether the Prepare pass runs.
func (c CheckMode) AtRuntime() bool {
	return c == CHECK_RUNTIME || c == CHECK_RUNTIME_AND_BOOT
}

// TypesConfig governs how go_solid checks the Go props a template is rendered
// with against the type its component declares for them.
type TypesConfig struct {
	// Check selects which passes run. Defaults to DEFAULT_CHECK.
	Check CheckMode
}

var NIL_TYPES_CONFIG = &TypesConfig{ // null object
	Check: CHECK_UNSET,
}
