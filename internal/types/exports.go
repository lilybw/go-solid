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
type NotAComponentError struct {
	Component meta.QualifiedName
	Path      meta.AbsoluteFilePath
	Export    meta.ExportName // "" for the default export
	Detail    string
	Exported  []meta.ExportName
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

func IsNotAComponent(err error) bool {
	var target *NotAComponentError
	return errors.As(err, &target)
}

// VerifyComponentExport reports whether comp names something renderable.
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
// behind it.
func exportsName(file *ast.SourceFile, name meta.ExportName) bool {
	if name == "" {
		return false
	}
	for stmt := range topLevel(file) {
		switch stmt.Kind {
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

		default:
			if slices.Contains(declarationKinds, stmt.Kind) &&
				exportedNotDefault(stmt) && named(stmt, name) {
				return true
			}
		}
	}
	return false
}

// ExportedComponents lists the named exports of a file that could be rendered.
func (e *Extractor) ExportedComponents(path meta.AbsoluteFilePath) ([]meta.ExportName, error) {
	file, err := e.file(path)
	if err != nil {
		return nil, err
	}
	return exportedComponentNames(file.tree), nil
}

// -----------------------------------------------------------------------------
// Syntax probes
// -----------------------------------------------------------------------------

func hasDefaultExport(file *ast.SourceFile) bool {
	for stmt := range topLevel(file) {
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
	var names []meta.ExportName
	add := func(name meta.ExportName) {
		if name != "" && name != meta.DEFAULT_EXPORT && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	for stmt := range topLevel(file) {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if exportedNotDefault(stmt) {
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
func exportClauseNames(stmt *ast.Node) []meta.ExportName {
	out := make([]meta.ExportName, 0, 4)
	for _, spec := range exportSpecifiers(stmt) {
		out = append(out, spec.public)
	}
	return out
}

// namedExportedFunction finds the function a file exports under name, whether
// declared with the export, bound to an exported variable, or re-exported from
// an `export { ... }` clause.
func namedExportedFunction(file *ast.SourceFile, name meta.ExportName) *ast.Node {
	if name == "" {
		return nil
	}
	for stmt := range topLevel(file) {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if exportedNotDefault(stmt) && named(stmt, name) {
				return stmt
			}
		case ast.KindVariableStatement:
			if !isExported(stmt) {
				continue
			}
			for decl := range boundNames(stmt) {
				if named(decl, name) {
					if fn := initializedFunction(decl); fn != nil {
						return fn
					}
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

type exportSpecifier struct{ local, public meta.ExportName }

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
	for stmt := range topLevel(file) {
		if stmt.Kind == ast.KindVariableStatement {
			for decl := range boundNames(stmt) {
				if named(decl, name) {
					return true
				}
			}
			continue
		}
		if slices.Contains(declarationKinds, stmt.Kind) && named(stmt, name) {
			return true
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
