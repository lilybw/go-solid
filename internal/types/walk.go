package types

import (
	"iter"

	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
)

// topLevel iterates a file's top-level statements, and nothing at all for a
// file that failed to parse into any.
func topLevel(file *ast.SourceFile) iter.Seq[*ast.Node] {
	return func(yield func(*ast.Node) bool) {
		if file == nil || file.Statements == nil {
			return
		}
		for _, stmt := range file.Statements.Nodes {
			if !yield(stmt) {
				return
			}
		}
	}
}

// ofKind narrows a statement sequence to the given kinds.
//
//	for stmt := range ofKind(topLevel(file), ast.KindFunctionDeclaration) { ... }
func ofKind(seq iter.Seq[*ast.Node], kinds ...ast.Kind) iter.Seq[*ast.Node] {
	return func(yield func(*ast.Node) bool) {
		for stmt := range seq {
			for _, kind := range kinds {
				if stmt.Kind == kind {
					if !yield(stmt) {
						return
					}
					break
				}
			}
		}
	}
}

// named reports whether a node declares the given name.
func named(node *ast.Node, name string) bool {
	declared := node.Name()
	return declared != nil && declared.Text() == name
}

func isExported(node *ast.Node) bool {
	return node.ModifierFlags()&ast.ModifierFlagsExport != 0
}

func isDefaultExported(node *ast.Node) bool {
	const both = ast.ModifierFlagsExportDefault
	return node.ModifierFlags()&both == both
}

// exportedNotDefault reports the `export`ed statements that are not the
// default export, which is the set a named selector can pick from.
func exportedNotDefault(node *ast.Node) bool {
	return isExported(node) && !isDefaultExported(node)
}

// declarationKinds are the top-level forms that bind a name other than through
// a variable statement.
var declarationKinds = []ast.Kind{
	ast.KindFunctionDeclaration, ast.KindClassDeclaration,
	ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration,
}

// variableDeclarations returns the declarations of a `const`/`let`/`var`
// statement, which may bind several names at once.
func variableDeclarations(variableStatement *ast.Node) []*ast.Node {
	if variableStatement == nil || variableStatement.Kind != ast.KindVariableStatement {
		return nil
	}
	list := variableStatement.AsVariableStatement().DeclarationList
	if list == nil {
		return nil
	}
	decls := list.AsVariableDeclarationList().Declarations
	if decls == nil {
		return nil
	}
	return decls.Nodes
}

// boundNames iterates every name a variable statement binds.
func boundNames(variableStatement *ast.Node) iter.Seq[*ast.Node] {
	return func(yield func(*ast.Node) bool) {
		for _, decl := range variableDeclarations(variableStatement) {
			if bound := decl.Name(); bound != nil {
				if !yield(decl) {
					return
				}
			}
		}
	}
}

func isFunctionExpression(n *ast.Node) bool {
	return n.Kind == ast.KindArrowFunction || n.Kind == ast.KindFunctionExpression
}

// initializedFunction returns the function a variable declaration binds, or nil.
func initializedFunction(decl *ast.Node) *ast.Node {
	init := decl.Initializer()
	if init != nil && isFunctionExpression(init) {
		return init
	}
	return nil
}
