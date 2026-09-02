package code_gen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lilybw/go-solid/internal/hashing"
	types_int "github.com/lilybw/go-solid/internal/types"
	"github.com/lilybw/go-solid/shared/meta"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
)

// ANONYMOUS_PREFIX opens the name derived for an inline component.
const ANONYMOUS_PREFIX = "Anon_"

// anonymousFileName is the name the fragment is parsed under; the extension
// selects the dialect, so it must stay .tsx.
const anonymousFileName = "anonymous.tsx"

// exportModifiers matches the modifiers a declaration cannot keep once it is
// rewritten into the initializer of a binding.
var exportModifiers = regexp.MustCompile(`^export\s+(default\s+)?`)

// Anonymous is one inline fragment made addressable: a module, and the name it
// exports the component under.
type Anonymous struct {
	Export meta.JSIdentifier
	Module string
}

// NormalizeAnonymous rewrites an inline fragment into a module exporting its
// component. Anything the fragment declares before that component is kept
// verbatim, so imports and helpers written beside it still apply.
//
// The export is named after the fragment's own text, so the same fragment is
// always the same component and a changed one is always a new one.
//
//	NormalizeAnonymous("<p>hi</p>")
//	// {Export: "Anon_0f2c1a", Module: "export const Anon_0f2c1a = (props) => (<p>hi</p>);"}
func NormalizeAnonymous(source string) (Anonymous, error) {
	if strings.TrimSpace(source) == "" {
		return Anonymous{}, fmt.Errorf("go_solid: anonymous component source is empty")
	}

	component := lastStatement(types_int.Parse(anonymousFileName, source))
	if component == nil {
		return Anonymous{}, fmt.Errorf("go_solid: anonymous component source declares nothing")
	}

	kept, value, err := componentValue(source, component)
	if err != nil {
		return Anonymous{}, err
	}

	export := ANONYMOUS_PREFIX + hashing.Short(source, 12)
	module := fmt.Sprintf("export const %s = %s;\n", export, value)
	if kept = strings.TrimRight(kept, " \t\n"); kept != "" {
		module = kept + "\n\n" + module
	}
	return Anonymous{Export: export, Module: module}, nil
}

// componentValue splits the fragment into the text before its component and the
// expression to bind that component to.
//
// The trailing declaration is always rebound rather than aliased: an alias holds
// an identifier, and the export a render resolves has to hold the function
// itself.
func componentValue(source string, component *ast.Node) (kept, value string, err error) {
	switch component.Kind {
	case ast.KindVariableStatement: // const C = <expression>
		declarations := variableDeclarations(component)
		if len(declarations) == 0 {
			break
		}
		initializer := declarations[len(declarations)-1].Initializer()
		if initializer == nil {
			break
		}
		return source[:component.Pos()], sourceRange(source, initializer), nil

	case ast.KindFunctionDeclaration, ast.KindClassDeclaration: // function C() {}
		// Dropping the modifiers turns the declaration into the expression the
		// binding takes; the name it may carry stays with it harmlessly.
		return source[:component.Pos()], exportModifiers.ReplaceAllString(sourceRange(source, component), ""), nil

	case ast.KindExportAssignment: // export default <expression>
		expression := component.AsExportAssignment().Expression
		if expression == nil {
			break
		}
		return source[:component.Pos()], sourceRange(source, expression), nil

	case ast.KindExpressionStatement: // <expression>
		expression := component.AsExpressionStatement().Expression
		if expression == nil {
			break
		}
		text := sourceRange(source, expression)
		if isJSX(unparenthesized(expression)) {
			text = "(props) => (" + text + ")"
		}
		return source[:expression.Pos()], text, nil
	}
	return "", "", fmt.Errorf("go_solid: an anonymous component cannot be made from %s", sourceRange(source, component))
}

// lastStatement returns the statement a fragment ends on, ignoring the empty
// ones a stray semicolon leaves behind.
func lastStatement(file *ast.SourceFile) *ast.Node {
	if file == nil || file.Statements == nil {
		return nil
	}
	statements := file.Statements.Nodes
	for i := len(statements) - 1; i >= 0; i-- {
		if statements[i].Kind != ast.KindEmptyStatement {
			return statements[i]
		}
	}
	return nil
}

func variableDeclarations(statement *ast.Node) []*ast.Node {
	list := statement.AsVariableStatement().DeclarationList
	if list == nil {
		return nil
	}
	declarations := list.AsVariableDeclarationList().Declarations
	if declarations == nil {
		return nil
	}
	return declarations.Nodes
}

// unparenthesized follows an expression through the parentheses around it, so a
// wrapped fragment is classified by what it actually is.
func unparenthesized(expression *ast.Node) *ast.Node {
	for expression != nil && expression.Kind == ast.KindParenthesizedExpression {
		expression = expression.AsParenthesizedExpression().Expression
	}
	return expression
}

func isJSX(expression *ast.Node) bool {
	if expression == nil {
		return false
	}
	switch expression.Kind {
	case ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment:
		return true
	}
	return false
}

// sourceRange slices a node out of the fragment. A node's position includes its
// leading trivia, which is not part of the value.
func sourceRange(source string, node *ast.Node) string {
	start, end := node.Pos(), node.End()
	if start < 0 || end > len(source) || start > end {
		return ""
	}
	return strings.TrimSpace(source[start:end])
}
