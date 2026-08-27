package types

import (
	"errors"
	"fmt"
	"strings"

	log_int "github.com/lilybw/go-solid/internal/logging"
	logging "github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/registry"
	. "github.com/lilybw/go-solid/shared/types"
)

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

func (k DiagnosticKind) Severity() Severity {
	switch k {
	case DIAG_UNTYPED:
		return SEVERITY_INFO
	default:
		return SEVERITY_ERROR
	}
}

// Diagnostic is one finding about a component's props.
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

type Reporter = func([]Diagnostic) error

// CoalesceDiagnostics is the default Reporter.
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

func (c *Checker) Cache() *Cache { return c.cache }

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
		// parameter or because its type could not be followed.
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
func (c *Checker) VerifyComponentExport(component *registry.Component) error {
	if c == nil || c.extractor == nil {
		return nil
	}
	return c.extractor.VerifyComponentExport(component)
}

func (c *Checker) extraction(component *registry.Component) (Extraction, bool) {
	if cached, ok := c.cache.Get(component.Name); ok {
		return cached, true
	}
	extracted, err := c.extractor.Component(component.Path, component.Export)
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
