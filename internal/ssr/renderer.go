// Package ssr renders the segment programs the compiler describes.
//
// The compiler says what a component's markup is and which of its holes can
// be filled from props; this package fills them. No JavaScript is executed:
// a hole the compiler could not describe is left empty and the client builds
// it, which makes server rendering a first paint rather than a hydration
// target.
package ssr

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/lilybw/go-solid-compiler/solid"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/go-solid/shared/registry"
	. "github.com/lilybw/go-solid/shared/ssr"
)

// Renderer produces first-paint markup for registered components.
//
//	markup, err := renderer.Markup(comp, props)
//	if err != nil { /* the component needs the client; render the shell empty */ }
type Renderer struct {
	cfg      *SSRConfig
	programs *programs
}

// NewRenderer returns a renderer for cfg. A nil or disabled config yields a
// renderer whose methods are all no-ops, so callers need not branch.
func NewRenderer(cfg *SSRConfig) *Renderer {
	return &Renderer{cfg: cfg, programs: newPrograms()}
}

// Active reports whether server rendering is on.
func (r *Renderer) Active() bool { return r != nil && r.cfg.Active() }

// Invalidate drops the analysis of a component whose source changed.
func (r *Renderer) Invalidate(component meta.QualifiedName) {
	if r == nil {
		return
	}
	r.programs.forget(component)
}

// Unrenderable reports why a component cannot be server-rendered, or nil when
// it can. The reasons come from the compiler and name the expressions it
// could not describe.
func (r *Renderer) Unrenderable(comp *registry.Component) []string {
	if !r.Active() || comp == nil {
		return nil
	}
	program, err := r.programs.get(comp)
	if err != nil {
		return []string{err.Error()}
	}
	if program.Green() {
		return nil
	}
	out := make([]string, 0, len(program.Red))
	for _, red := range program.Red {
		out = append(out, fmt.Sprintf("%s (%s)", red.Code, red.Reason))
	}
	return out
}

// OnPrepare fails a component that cannot be fully server-rendered, but only
// under Strict. Otherwise an unrenderable component is an ordinary fall back
// to client rendering, not an error.
func (r *Renderer) OnPrepare(comp *registry.Component) error {
	if r == nil || !r.cfg.IsStrict() {
		return nil
	}
	reasons := r.Unrenderable(comp)
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf(
		"go_solid: %q cannot be server-rendered: %s (set SSR.Strict to false to "+
			"fall back to client rendering instead)",
		comp.Name, strings.Join(reasons, "; "))
}

// Inert reports whether a component's markup is complete and needs no client
// JavaScript at all, so its bundle could be dropped from the document.
func (r *Renderer) Inert(comp *registry.Component) bool {
	if !r.Active() || comp == nil {
		return false
	}
	program, err := r.programs.get(comp)
	return err == nil && program.Inert()
}

// Markup renders a component's first paint. It reports an error rather than
// partial markup when any hole needs the client, which leaves the caller to
// serve an empty mount point.
func (r *Renderer) Markup(comp *registry.Component, props map[string]any) (string, error) {
	if !r.Active() || comp == nil {
		return "", nil
	}
	program, err := r.programs.get(comp)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := render(&b, program, props); err != nil {
		return "", err
	}
	return b.String(), nil
}

// render writes a program's markup for one set of props.
func render(w io.Writer, program *solid.Program, props map[string]any) error {
	if !program.Green() {
		return fmt.Errorf("go_solid/ssr: %d hole(s) need the client", len(program.Red))
	}
	for _, seg := range program.Segments {
		if err := writeSegment(w, seg, props); err != nil {
			return err
		}
	}
	return nil
}

func writeSegment(w io.Writer, seg solid.Segment, props map[string]any) error {
	switch x := seg.(type) {
	case *solid.Static:
		_, err := io.WriteString(w, x.HTML)
		return err

	case *solid.Hole:
		s, ok, err := value(x.Slot, props)
		if err != nil || !ok {
			return err
		}
		_, err = io.WriteString(w, solid.EscapeValue(s, solid.EscapeText))
		return err

	case *solid.Attribute:
		v, err := solid.Eval(x.Slot.Plan, props)
		if err != nil {
			return err
		}
		return writeAttribute(w, x, v)
	}
	return fmt.Errorf("go_solid/ssr: unknown segment %T", seg)
}

// value evaluates a slot as character data, reporting false for the values
// JSX writes nothing for.
func value(slot solid.Slot, props map[string]any) (string, bool, error) {
	v, err := solid.Eval(slot.Plan, props)
	if err != nil {
		return "", false, err
	}
	s, ok := solid.Display(v)
	return s, ok, nil
}

// writeAttribute applies the attribute rules, which are not the rules for
// children: true writes an attribute where it would write no child, and a
// boolean attribute carries its value by presence rather than by content.
func writeAttribute(w io.Writer, attr *solid.Attribute, v any) error {
	if attr.Bool {
		if !solid.Truthy(v) {
			return nil
		}
		_, err := io.WriteString(w, " "+attr.Name)
		return err
	}
	if v == nil || v == solid.Undef {
		return nil // an absent value omits the attribute
	}
	if b, ok := v.(bool); ok && !b {
		return nil
	}
	_, err := io.WriteString(w,
		" "+attr.Name+`="`+solid.EscapeValue(solid.ToString(v), solid.EscapeAttr)+`"`)
	return err
}
