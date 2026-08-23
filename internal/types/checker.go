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
	case DIAG_UNTYPED:
		return "untyped"
	case DIAG_UNDERIVABLE:
		return "underivable"
	default:
		return "unknown"
	}
}

// Severity separates a contract that was broken from coverage that is absent.
type Severity uint8

const (
	// SEVERITY_INFO: nothing is wrong, something is merely unchecked.
	SEVERITY_INFO Severity = iota
	// SEVERITY_ERROR: the component states a requirement the props do not meet.
	SEVERITY_ERROR
)

// Severity of a finding.
//
// A component that declares no props type states no requirement, so nothing
// can violate it — that is a gap in coverage, not a fault, and it must not stop
// a boot or a render. Only a requirement the props fail to meet is an error.
func (k DiagnosticKind) Severity() Severity {
	switch k {
	case DIAG_UNTYPED:
		return SEVERITY_INFO
	default:
		return SEVERITY_ERROR
	}
}

// Diagnostic is one finding about a component's props.
//
// What a batch of them means is the Reporter's decision. Under the default
// Reporter a fault fails the pass it was raised in — the boot pass fails New,
// the runtime pass fails that render — while a finding that only reports absent
// coverage is logged and passes.
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

// Reporter receives the diagnostics of one pass, which may be empty, and
// decides what they mean. Returning an error fails the pass: the boot pass
// fails New, the runtime pass fails the render it was called for.
type Reporter = func([]Diagnostic) error

// CoalesceDiagnostics is the default Reporter.
//
// Faults are gathered into a single error, so a caller is shown every one in
// the batch rather than whichever came first. Findings that only report absent
// coverage are logged and pass, because a component that states no contract
// cannot have broken one.
func CoalesceDiagnostics(diagnostics []Diagnostic) error {
	var faults []Diagnostic
	for _, d := range diagnostics {
		if d.Kind.Severity() == SEVERITY_ERROR {
			faults = append(faults, d)
			continue
		}
		log_int.Log(logging.LEVEL_INFO, "[go_solid/types] "+d.String())
	}
	if len(faults) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("[go_solid/types] type diagnostics:")
	for _, d := range faults {
		b.WriteString("\n  " + d.String())
	}
	return errors.New(b.String())
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
	coalesce  Reporter
}

// NewChecker roots a checker on a workspace. A nil report defaults to
// CoalesceDiagnostics.
func NewChecker(workspace meta.AbsoluteDirectoryPath, mode CheckMode, report Reporter) *Checker {
	if report == nil {
		report = CoalesceDiagnostics
	}
	return &Checker{
		mode:      mode,
		cache:     NewCache(workspace),
		extractor: NewExtractor(),
		coalesce:  report,
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
func (c *Checker) OnPrepare(component *registry.Component, props any) error {
	if c == nil || component == nil || props == nil || !c.mode.AtRuntime() {
		return nil
	}

	shape, err := c.mapper.ShapeOfValue(props)
	if err != nil {
		// An absent props value is ordinary, and one json cannot encode fails
		// the render loudly on its own.
		if errors.Is(err, ErrNoProps) || errors.Is(err, ErrUnmarshalable) {
			return nil
		}
		return c.coalesce([]Diagnostic{{
			Component: component.Name,
			Kind:      DIAG_UNDERIVABLE,
			Detail:    err.Error(),
		}})
	}

	extraction, ok := c.extraction(component)
	if !ok || !extraction.Found {
		// No resolvable contract, either because the component takes no
		// parameter or because its type could not be followed. Supplying more
		// than is required is always allowed, and a component that requires
		// nothing is the limiting case of that.
		return nil
	}

	detail := ""
	if len(extraction.Unresolved) > 0 {
		detail = "partially checked; could not resolve " + strings.Join(extraction.Unresolved, ", ")
	}
	diagnostics := wrap(component.Name, DIAG_PROPS, Violations(extraction.Shape, shape))
	for i := range diagnostics {
		diagnostics[i].Detail = detail
	}
	return c.coalesce(diagnostics)
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
func (c *Checker) OnBoot(components []*registry.Component) error {
	if c == nil {
		return nil
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
	return c.coalesce(diagnostics)
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
