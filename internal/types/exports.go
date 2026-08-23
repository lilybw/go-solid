package types

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/go-solid/shared/registry"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
)

// Resolution of a selector to an export
// -----------------------------------------------------------------------------
// A selector names a file and, after "#", an export to take out of it. What the
// file actually exports is only knowable by parsing it, so that answer lives
// here rather than in the registry, which never reads a component's contents.
//
// Nothing below decides whether a component's props are correct. It decides
// whether there is a component there at all, which is a question every render
// asks and type checking only sharpens.

// NotAComponentError says a selector resolved to a file, but not to something
// that can be rendered. Detail carries what is wrong; Exported lists the names
// the file does offer, so a caller can suggest them.
type NotAComponentError struct {
	Component meta.QualifiedName
	Path      meta.AbsoluteFilePath
	Export    string // "" for the default export
	Detail    string
	Exported  []string
}

func (e *NotAComponentError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "component %q: %s", e.Component, e.Detail)
	if len(e.Exported) > 0 {
		fmt.Fprintf(&b, "; %s exports %s (select one with %q)",
			shortPath(e.Path), strings.Join(e.Exported, ", "),
			e.Component+meta.EXPORT_SELECTOR+e.Exported[0])
	}
	return b.String()
}

// IsNotAComponent reports whether err says a file backs no renderable
// component. Rasterization uses this to walk past helper modules that happen to
// live in the components tree instead of failing over them.
func IsNotAComponent(err error) bool {
	var target *NotAComponentError
	return errors.As(err, &target)
}

// VerifyComponentExport reports whether comp names something renderable.
//
// A file with no default export is not an error in itself — it may hold several
// named components — so the message names what it does export.
func (e *Extractor) VerifyComponentExport(comp *registry.Component) error {
	if !meta.ValidExportName(comp.Export) {
		return &NotAComponentError{
			Component: comp.Name, Path: comp.Path, Export: comp.Export,
			Detail: fmt.Sprintf("%q is not a name that can be imported", comp.Export),
		}
	}

	file, err := e.file(comp.Path)
	if err != nil {
		return err
	}
	exported := exportedComponentNames(file.tree)

	fail := func(detail string) error {
		return &NotAComponentError{
			Component: comp.Name, Path: comp.Path, Export: comp.Export,
			Detail: detail, Exported: exported,
		}
	}

	if comp.Export == "" {
		switch {
		case defaultExportedFunction(file.tree) != nil:
			return nil
		case hasDefaultExport(file.tree):
			return fail(fmt.Sprintf("the default export of %s is not a component", shortPath(comp.Path)))
		default:
			return fail(fmt.Sprintf("no default export in %s", shortPath(comp.Path)))
		}
	}

	// Order matters, and so does asking the right question at each step.
	// exportsName is value-agnostic on purpose: exported-but-not-a-component
	// and not-exported-at-all are different mistakes, and the component list
	// cannot tell them apart because it only holds things that could be
	// components.
	switch {
	case namedExportedFunction(file.tree, comp.Export) != nil:
		return nil
	case exportsName(file.tree, comp.Export):
		return fail(fmt.Sprintf("%s exports %q, but it is not a component", shortPath(comp.Path), comp.Export))
	case declaresName(file.tree, comp.Export):
		return fail(fmt.Sprintf("%s declares %q but does not export it", shortPath(comp.Path), comp.Export))
	default:
		return fail(fmt.Sprintf("%s has no exported %q", shortPath(comp.Path), comp.Export))
	}
}

// exportsName reports whether the file exports name at all, whatever the value
// behind it. Distinct from exportedComponentNames, which lists only the exports
// that could be rendered.
func exportsName(file *ast.SourceFile, name string) bool {
	if file.Statements == nil || name == "" {
		return false
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration,
			ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			if !isExported(stmt) || isDefaultExported(stmt) {
				continue
			}
			if declared := stmt.Name(); declared != nil && declared.Text() == name {
				return true
			}
		case ast.KindVariableStatement:
			if !isExported(stmt) {
				continue
			}
			for _, decl := range variableDeclarations(stmt) {
				if bound := decl.Name(); bound != nil && bound.Text() == name {
					return true
				}
			}
		case ast.KindExportDeclaration:
			for _, spec := range exportSpecifiers(stmt) {
				if spec.public == name {
					return true
				}
			}
		}
	}
	return false
}

// ExportedComponents lists the named exports of a file that could be rendered.
// The default export, when there is one, is not listed: it is what a selector
// with no "#" already names.
func (e *Extractor) ExportedComponents(path meta.AbsoluteFilePath) ([]string, error) {
	file, err := e.file(path)
	if err != nil {
		return nil, err
	}
	return exportedComponentNames(file.tree), nil
}

// -----------------------------------------------------------------------------
// Syntax probes
// -----------------------------------------------------------------------------

func isExported(node *ast.Node) bool {
	return node.ModifierFlags()&ast.ModifierFlagsExport != 0
}

func isDefaultExported(node *ast.Node) bool {
	const both = ast.ModifierFlagsExportDefault
	return node.ModifierFlags()&both == both
}

// hasDefaultExport reports whether the file exports a default at all, whatever
// its value. It separates "there is nothing there" from "what is there is not a
// component", which are different mistakes with different fixes.
func hasDefaultExport(file *ast.SourceFile) bool {
	if file.Statements == nil {
		return false
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindExportAssignment:
			if stmt.AsExportAssignment().Expression != nil {
				return true
			}
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration:
			if isDefaultExported(stmt) {
				return true
			}
		case ast.KindExportDeclaration:
			if slices.Contains(exportClauseNames(stmt), meta.DEFAULT_EXPORT) {
				return true
			}
		}
	}
	return false
}

// exportedComponentNames lists the named exports whose value is a function,
// sorted, since that is the set a selector can pick from.
func exportedComponentNames(file *ast.SourceFile) []string {
	if file.Statements == nil {
		return nil
	}
	var names []string
	add := func(name string) {
		if name != "" && name != meta.DEFAULT_EXPORT && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if isExported(stmt) && !isDefaultExported(stmt) {
				if name := stmt.Name(); name != nil {
					add(name.Text())
				}
			}
		case ast.KindVariableStatement:
			if !isExported(stmt) {
				continue
			}
			for _, decl := range variableDeclarations(stmt) {
				init := decl.Initializer()
				name := decl.Name()
				if init != nil && isFunctionExpression(init) && name != nil {
					add(name.Text())
				}
			}
		case ast.KindExportDeclaration:
			// `export { Local as Public }` re-exports whatever Local is; the
			// value is only knowable by following the binding.
			for _, name := range exportClauseNames(stmt) {
				if boundFunction(file, name) != nil || namedExportedFunction(file, name) != nil {
					add(name)
				}
			}
		}
	}
	slices.Sort(names)
	return names
}

// exportClauseNames returns the outward-facing names of an `export { ... }`.
func exportClauseNames(stmt *ast.Node) []string {
	out := make([]string, 0, 4)
	for _, spec := range exportSpecifiers(stmt) {
		out = append(out, spec.public)
	}
	return out
}

// namedExportedFunction finds the function a file exports under name, whether
// declared with the export, bound to an exported variable, or re-exported from
// an `export { ... }` clause.
func namedExportedFunction(file *ast.SourceFile, name string) *ast.Node {
	if file.Statements == nil || name == "" {
		return nil
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if !isExported(stmt) || isDefaultExported(stmt) {
				continue
			}
			if declared := stmt.Name(); declared != nil && declared.Text() == name {
				return stmt
			}
		case ast.KindVariableStatement:
			if !isExported(stmt) {
				continue
			}
			for _, decl := range variableDeclarations(stmt) {
				bound := decl.Name()
				init := decl.Initializer()
				if bound != nil && bound.Text() == name && init != nil && isFunctionExpression(init) {
					return init
				}
			}
		case ast.KindExportDeclaration:
			for _, spec := range exportSpecifiers(stmt) {
				if spec.public != name {
					continue
				}
				if fn := boundFunction(file, spec.local); fn != nil {
					return fn
				}
			}
		}
	}
	return nil
}

type exportSpecifier struct{ local, public string }

// exportSpecifiers pairs each `export { Local as Public }` element with the
// binding it points at. Without a property name the two are the same.
func exportSpecifiers(stmt *ast.Node) []exportSpecifier {
	decl := stmt.AsExportDeclaration()
	if decl == nil || decl.ExportClause == nil || decl.ModuleSpecifier != nil {
		return nil // `export ... from "..."` re-exports another module, not this one
	}
	clause := decl.ExportClause
	if clause.Kind != ast.KindNamedExports {
		return nil // `export * as ns from ...` names no individual binding
	}
	elements := clause.AsNamedExports().Elements
	if elements == nil {
		return nil
	}
	out := make([]exportSpecifier, 0, len(elements.Nodes))
	for _, spec := range elements.Nodes {
		public := spec.Name()
		if public == nil {
			continue
		}
		local := public.Text()
		if property := spec.AsExportSpecifier().PropertyName; property != nil {
			local = property.Text()
		}
		out = append(out, exportSpecifier{local: local, public: public.Text()})
	}
	return out
}

// declaresName reports whether name is declared at the top level of the file.
// Reached only after exportsName has said no, so in practice it answers "is
// this a missing export keyword rather than a typo".
func declaresName(file *ast.SourceFile, name string) bool {
	if file.Statements == nil {
		return false
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration, ast.KindClassDeclaration,
			ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			if declared := stmt.Name(); declared != nil && declared.Text() == name {
				return true
			}
		case ast.KindVariableStatement:
			for _, decl := range variableDeclarations(stmt) {
				if bound := decl.Name(); bound != nil && bound.Text() == name {
					return true
				}
			}
		}
	}
	return false
}

// shortPath keeps an error readable: the file and its parent, not the machine
// it was built on.
func shortPath(path meta.AbsoluteFilePath) string {
	slashed := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(slashed, "/")
	if len(parts) <= 2 {
		return slashed
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
