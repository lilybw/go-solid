package types

import (
	"errors"
	"fmt"
	"strings"

	log_int "github.com/lilybw/go-solid/internal/logging"
	"github.com/lilybw/go-solid/internal/meta"
	logging "github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/registry"
	. "github.com/lilybw/go-solid/shared/types"
)

// DiagnosticKind classifies one finding.
type DiagnosticKind uint8

const (
	// DIAG_PROPS: the Go props do not satisfy the props type the component
	// declares.
	DIAG_PROPS DiagnosticKind = iota
	// DIAG_NO_PROPS: props were supplied to a component that takes no
	// parameter, so nothing will read them.
	DIAG_NO_PROPS
	// DIAG_UNTYPED: the component's props type could not be resolved, so
	// nothing can be checked against it.
	DIAG_UNTYPED
	// DIAG_UNDERIVABLE: the props value does not marshal to a JSON object.
	DIAG_UNDERIVABLE
)

func (k DiagnosticKind) String() string {
	switch k {
	case DIAG_PROPS:
		return "props"
	case DIAG_NO_PROPS:
		return "no-props"
	case DIAG_UNTYPED:
		return "untyped"
	case DIAG_UNDERIVABLE:
		return "underivable"
	default:
		return "unknown"
	}
}

// Diagnostic is one advisory finding about a component's props. Diagnostics are
// never fatal: a render that produced one still renders.
type Diagnostic struct {
	Component meta.QualifiedName
	Kind      DiagnosticKind
	Detail    string
	Violation *Violation
}

func (d Diagnostic) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s]", d.Component, d.Kind)
	if d.Violation != nil {
		v := *d.Violation
		switch v.Kind {
		case VIOLATION_MISSING:
			fmt.Fprintf(&b, " required %q (%s) is not supplied", v.Field, v.Want)
		case VIOLATION_OPTIONAL:
			fmt.Fprintf(&b, " required %q may be absent", v.Field)
		default:
			fmt.Fprintf(&b, " %q: needs %s, supplied as %s", v.Field, v.Want, v.Got)
		}
	}
	if d.Detail != "" {
		b.WriteString(" (" + d.Detail + ")")
	}
	return b.String()
}

// Reporter receives diagnostics. It is called with a non-empty slice.
type Reporter = func([]Diagnostic)

// LogDiagnostics is the default Reporter. It logs at LEVEL_ERROR so findings
// are visible under the default log level, though nothing here is fatal.
func LogDiagnostics(diagnostics []Diagnostic) {
	for _, d := range diagnostics {
		log_int.Log(logging.LEVEL_ERROR, "[go_solid/types] "+d.String())
	}
}

// Checker holds the props passed to a template against the type the component
// declares for them.
//
// The component file is the contract. Whatever it says its props are is what
// the Go side has to satisfy, and the correlation is covariant: props may carry
// more than the component reads, in any order, but every field the component
// requires has to be there at a compatible type.
//
// Extracted shapes are cached under CACHE_DIR_NAME whatever CheckMode holds;
// the mode governs only whether findings are reported.
type Checker struct {
	mode      CheckMode
	cache     *Cache
	extractor *Extractor
	mapper    Mapper
	report    Reporter
}

// NewChecker roots a checker on a workspace. A nil report defaults to
// LogDiagnostics.
func NewChecker(workspace meta.AbsoluteDirectoryPath, mode CheckMode, report Reporter) *Checker {
	if report == nil {
		report = LogDiagnostics
	}
	return &Checker{
		mode:      mode,
		cache:     NewCache(workspace),
		extractor: NewExtractor(),
		report:    report,
	}
}

// Cache exposes the underlying cache, for tests and for callers that need to
// know where an entry landed.
func (c *Checker) Cache() *Cache { return c.cache }

// Invalidate drops what is remembered about a component. HMR calls it when a
// component is rebuilt; the cache would notice the change on its own, but not
// until the next render asks.
func (c *Checker) Invalidate(component meta.QualifiedName) {
	if c == nil {
		return
	}
	c.cache.Invalidate(component)
}

// OnPrepare holds props against the component's declared type.
func (c *Checker) OnPrepare(component *registry.Component, props any) {
	if c == nil || component == nil || props == nil || !c.mode.AtRuntime() {
		return
	}

	shape, err := c.mapper.ShapeOfValue(props)
	if err != nil {
		// Nothing to check, and nothing worth saying: an absent props value is
		// ordinary, and one json cannot encode fails the render loudly already.
		if errors.Is(err, ErrNoProps) || errors.Is(err, ErrUnmarshalable) {
			return
		}
		c.emit([]Diagnostic{{
			Component: component.Name,
			Kind:      DIAG_UNDERIVABLE,
			Detail:    err.Error(),
		}})
		return
	}

	extraction, ok := c.extraction(component)
	if !ok {
		return
	}
	if !extraction.HasParameter {
		if shape.Empty() {
			return
		}
		c.emit([]Diagnostic{{
			Component: component.Name,
			Kind:      DIAG_NO_PROPS,
			Detail:    "the component takes no parameter, so the props are unreachable",
		}})
		return
	}
	if !extraction.Found {
		return // reported once at boot rather than on every render
	}

	detail := ""
	if len(extraction.Unresolved) > 0 {
		detail = "partially checked; could not resolve " + strings.Join(extraction.Unresolved, ", ")
	}
	diagnostics := wrap(component.Name, DIAG_PROPS, Violations(extraction.Shape, shape))
	for i := range diagnostics {
		diagnostics[i].Detail = detail
	}
	c.emit(diagnostics)
}

// OnBoot extracts and caches every component's props type, and drops entries
// for components that no longer exist.
//
// Extraction runs whatever CheckMode holds, so the cache is warm before the
// first request. With the boot pass on, it also names the components whose
// props type could not be resolved: those are the ones the runtime pass will
// have nothing to hold props against, and saying so once at startup beats
// silence.
//
// The pass reads and parses component sources; it neither bundles nor renders,
// so it is independent of rasterization.
func (c *Checker) OnBoot(components []*registry.Component) {
	if c == nil {
		return
	}

	names := make([]meta.QualifiedName, 0, len(components))
	for _, component := range components {
		names = append(names, component.Name)
	}
	if removed, err := c.cache.Prune(names); err != nil {
		log_int.Log(logging.LEVEL_ERROR, "[go_solid/types] prune failed: "+err.Error())
	} else if removed > 0 {
		log_int.Log(logging.LEVEL_INFO, fmt.Sprintf("[go_solid/types] pruned %d orphaned cache entries", removed))
	}

	var diagnostics []Diagnostic
	for _, component := range components {
		extraction, ok := c.extraction(component)
		if !ok || !c.mode.AtBoot() {
			continue
		}
		if extraction.Found || !extraction.HasParameter {
			continue // typed, or takes no props at all
		}
		detail := "props are not checked for this component"
		if len(extraction.Unresolved) > 0 {
			detail = "could not resolve " + strings.Join(extraction.Unresolved, ", ")
		}
		diagnostics = append(diagnostics, Diagnostic{
			Component: component.Name,
			Kind:      DIAG_UNTYPED,
			Detail:    detail,
		})
	}
	c.emit(diagnostics)
}

// extraction resolves a component's props type, from cache when it is still
// valid and by reading the component when it is not.
func (c *Checker) extraction(component *registry.Component) (Extraction, bool) {
	if cached, ok := c.cache.Get(component.Name); ok {
		return cached, true
	}
	extracted, err := c.extractor.Component(component.Path)
	if err != nil {
		log_int.Log(logging.LEVEL_ERROR, fmt.Sprintf(
			"[go_solid/types] cannot read %q: %v", component.Name, err))
		return Extraction{}, false
	}
	if err := c.cache.Put(component.Name, extracted); err != nil {
		log_int.Log(logging.LEVEL_ERROR, "[go_solid/types] "+err.Error())
	}
	return extracted, true
}

func wrap(component meta.QualifiedName, kind DiagnosticKind, violations []Violation) []Diagnostic {
	out := make([]Diagnostic, 0, len(violations))
	for _, violation := range violations {
		out = append(out, Diagnostic{Component: component, Kind: kind, Violation: &violation})
	}
	return out
}

func (c *Checker) emit(diagnostics []Diagnostic) {
	if len(diagnostics) > 0 {
		c.report(diagnostics)
	}
}
