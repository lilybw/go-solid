package types

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	log_int "github.com/lilybw/go-solid/internal/logging"
	"github.com/lilybw/go-solid/internal/meta"
	logging "github.com/lilybw/go-solid/shared/logging"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/core"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/parser"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/tspath"
)

// Extraction is what a component file says about its props. The component is the source of truth
type Extraction struct {
	// Shape is the props object as declared. Meaningful only when Found.
	Shape Shape `json:"shape"`
	// Name is the referenced type's name, empty for an inline type literal.
	Name string `json:"name,omitempty"`
	// Found reports that a props parameter with a resolvable type was located.
	Found bool `json:"found"`
	// HasParameter reports that the component takes a parameter at all,
	// whether or not its type could be resolved.
	HasParameter bool `json:"hasParameter"`
	// Sources are the files the shape was read from, the component first.
	Sources []meta.AbsoluteFilePath `json:"sources"`
	// Unresolved names the types that could not be followed. A shape can be
	// Found with names still listed here: an intersection contributes every
	// arm it can resolve rather than collapsing because one arm is opaque.
	Unresolved []string `json:"unresolved,omitempty"`
}

// Extractor reads props types out of TypeScript and JSX sources.
//
// Resolution is syntactic — the parser only, no checker and no program
type Extractor struct {
	mu    sync.Mutex
	files map[meta.AbsoluteFilePath]*parsedFile
}

type parsedFile struct {
	path  meta.AbsoluteFilePath
	text  string
	tree  *ast.SourceFile
	stamp stamp
}

func NewExtractor() *Extractor {
	return &Extractor{files: make(map[meta.AbsoluteFilePath]*parsedFile)}
}

// Component resolves the props type declared by the component at path.
// Component reads the props type of a component in path.
func (e *Extractor) Component(path meta.AbsoluteFilePath, export string) (Extraction, error) {
	root, err := e.file(path)
	if err != nil {
		return Extraction{}, err
	}

	r := &resolver{
		extractor: e,
		sources:   map[meta.AbsoluteFilePath]bool{path: true},
		visited:   map[string]bool{},
	}
	out := Extraction{}

	fn := defaultExportedFunction(root.tree)
	if export != "" {
		fn = namedExportedFunction(root.tree, export)
	}
	if fn == nil {
		return r.finish(out), nil
	}
	params := fn.ParameterList()
	if params == nil || len(params.Nodes) == 0 {
		return r.finish(out), nil
	}
	out.HasParameter = true

	typeNode := params.Nodes[0].Type()
	if typeNode == nil {
		return r.finish(out), nil
	}
	out.Shape, out.Name, out.Found = r.shapeOfTypeNode(root, typeNode, 0)
	return r.finish(out), nil
}

// Forget drops the parsed copy of path.
func (e *Extractor) Forget(path meta.AbsoluteFilePath) {
	e.mu.Lock()
	delete(e.files, path)
	e.mu.Unlock()
}

func (e *Extractor) file(path meta.AbsoluteFilePath) (*parsedFile, error) {
	current, err := stampOf(path)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	hit, ok := e.files[path]
	e.mu.Unlock()
	if ok && hit.stamp == current {
		return hit, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	f := &parsedFile{path: path, text: text, tree: Parse(path, text), stamp: current}

	e.mu.Lock()
	e.files[path] = f
	e.mu.Unlock()
	return f, nil
}

// Parse builds a syntax tree for text, taking the dialect from path's extension.
func Parse(path meta.AbsoluteFilePath, text string) *ast.SourceFile {
	name := tspath.NormalizePath(path)
	if tspath.GetEncodedRootLength(name) == 0 {
		name = tspath.NormalizePath("/" + name)
	}
	return parser.ParseSourceFile(
		ast.SourceFileParseOptions{
			FileName: name,
			Path:     tspath.Path(name),
		},
		text,
		// ParseSourceFile panics on ScriptKindUnknown; Ensure- defaults to TS.
		core.EnsureScriptKindFromFileName(name),
	)
}

// TODO: Make this a setting in the config
// maxTypeResolutionDepth bounds alias and import chasing.
const maxTypeResolutionDepth = 8

// resolver carries the state of one extraction: the files it has touched, the
// names it could not follow, and a guard against a type that refers to itself.
type resolver struct {
	extractor  *Extractor
	sources    map[meta.AbsoluteFilePath]bool
	visited    map[string]bool
	unresolved []string
}

func (r *resolver) finish(out Extraction) Extraction {
	out.Sources = make([]meta.AbsoluteFilePath, 0, len(r.sources))
	for path := range r.sources {
		out.Sources = append(out.Sources, path)
	}
	slices.Sort(out.Sources)

	slices.Sort(r.unresolved)
	out.Unresolved = slices.Compact(r.unresolved)
	return out
}

func (r *resolver) note(name string) {
	if name != "" {
		r.unresolved = append(r.unresolved, name)
	}
}

func (r *resolver) shapeOfTypeNode(f *parsedFile, typeNode *ast.Node, depth int) (Shape, string, bool) {
	if typeNode == nil || depth > maxTypeResolutionDepth {
		return Shape{}, "", false
	}
	switch typeNode.Kind {
	case ast.KindTypeLiteral:
		return NewShape(membersToFields(f.text, typeNode.Members())), "", true

	case ast.KindTypeReference:
		nameNode := typeNode.AsTypeReferenceNode().TypeName
		if nameNode == nil || nameNode.Kind != ast.KindIdentifier {
			return Shape{}, "", false
		}
		name := nameNode.Text()
		shape, ok := r.resolveNamed(f, name, depth)
		return shape, name, ok

	case ast.KindIntersectionType:
		// Contribute every arm that resolves. An arm that does not is recorded
		// and skipped, because giving up on the whole intersection would turn
		// the check off for exactly the components that compose a generated
		// type into their props.
		types := typeNode.AsIntersectionTypeNode().Types
		if types == nil {
			return Shape{}, "", false
		}
		var (
			merged   []Field
			resolved bool
		)
		for _, arm := range types.Nodes {
			shape, name, ok := r.shapeOfTypeNode(f, arm, depth+1)
			if !ok {
				r.note(name)
				continue
			}
			resolved = true
			merged = append(merged, shape.fields...)
		}
		return NewShape(merged), "", resolved
	}
	return Shape{}, "", false
}

// resolveNamed follows a type name to its declaration, locally first and then
// through a named import.
func (r *resolver) resolveNamed(f *parsedFile, name string, depth int) (Shape, bool) {
	if depth > maxTypeResolutionDepth {
		return Shape{}, false
	}
	key := f.path + "#" + name
	if r.visited[key] {
		return Shape{}, false // a type that refers to itself
	}
	r.visited[key] = true
	defer delete(r.visited, key) // scoped to this path; see resolver

	if decl := topLevelType(f.tree, name); decl != nil {
		return r.shapeOfDeclaration(f, decl, depth)
	}

	target, exported, ok := r.importedFrom(f, name)
	if !ok {
		r.note(name)
		return Shape{}, false
	}
	imported, err := r.extractor.file(target)
	if err != nil {
		r.note(name)
		return Shape{}, false
	}
	r.sources[target] = true
	return r.resolveNamed(imported, exported, depth+1)
}

func (r *resolver) shapeOfDeclaration(f *parsedFile, decl *ast.Node, depth int) (Shape, bool) {
	switch decl.Kind {
	case ast.KindInterfaceDeclaration:
		return NewShape(membersToFields(f.text, decl.Members())), true
	case ast.KindTypeAliasDeclaration:
		shape, _, ok := r.shapeOfTypeNode(f, decl.AsTypeAliasDeclaration().Type, depth+1)
		return shape, ok
	}
	return Shape{}, false
}

// importedFrom finds the module a named type was imported from, and the name it
// is exported under there.
func (r *resolver) importedFrom(f *parsedFile, local string) (meta.AbsoluteFilePath, string, bool) {
	if f.tree.Statements == nil {
		return "", "", false
	}
	for _, stmt := range f.tree.Statements.Nodes {
		if stmt.Kind != ast.KindImportDeclaration {
			continue
		}
		decl := stmt.AsImportDeclaration()
		if decl.ImportClause == nil || decl.ModuleSpecifier == nil {
			continue
		}
		if decl.ModuleSpecifier.Kind != ast.KindStringLiteral {
			continue
		}
		bindings := decl.ImportClause.AsImportClause().NamedBindings
		if bindings == nil || bindings.Kind != ast.KindNamedImports {
			continue // a namespace or default import names no type directly
		}
		elements := bindings.AsNamedImports().Elements
		if elements == nil {
			continue
		}
		for _, spec := range elements.Nodes {
			if spec.Kind != ast.KindImportSpecifier {
				continue
			}
			bound := spec.Name()
			if bound == nil || bound.Text() != local {
				continue
			}
			exported := local
			if property := spec.AsImportSpecifier().PropertyName; property != nil {
				exported = property.Text()
			}
			specifier := decl.ModuleSpecifier.Text()
			target, ok := resolveModule(f.path, specifier)
			if !ok {
				// A bare specifier is skipped by design. A relative one that
				// resolves to nothing is a real miss — a moved definition, a
				// path typo, an extension the resolver does not know — and
				// saying so beats leaving the component quietly unchecked.
				if isRelativeSpecifier(specifier) {
					log_int.Log(logging.LEVEL_ERROR, fmt.Sprintf(
						"[go_solid/types] %s: cannot resolve %q, imported for type %q; "+
							"props against it are unchecked", f.path, specifier, local))
				}
				return "", "", false
			}
			return target, exported, true
		}
	}
	return "", "", false
}

// moduleExtensions are tried in TypeScript's order for a specifier that names
// no extension of its own.
var moduleExtensions = []string{".ts", ".tsx", ".d.ts", ".mts", ".cts"}

// outputExtensions map an emitted extension back to the sources that produce
// it.
var outputExtensions = map[string][]string{
	".js":  {".ts", ".tsx", ".d.ts"},
	".jsx": {".tsx"},
	".mjs": {".mts", ".d.mts"},
	".cjs": {".cts", ".d.cts"},
}

// resolveCandidates lists the files a relative specifier could name, in the
// order TypeScript would try them.
func resolveCandidates(base string) []string {
	candidates := make([]string, 0, 2*len(moduleExtensions)+len(outputExtensions)+1)

	if ext := filepath.Ext(base); ext != "" {
		stem := strings.TrimSuffix(base, ext)
		// The emitted name first, since that is what the specifier asked for.
		for _, source := range outputExtensions[ext] {
			candidates = append(candidates, stem+source)
		}
		candidates = append(candidates, base)
	}
	for _, ext := range moduleExtensions {
		candidates = append(candidates, base+ext)
	}
	for _, ext := range moduleExtensions {
		candidates = append(candidates, filepath.Join(base, "index"+ext))
	}
	return candidates
}

// resolveModule turns a relative module specifier into a file.
func resolveModule(from meta.AbsoluteFilePath, specifier string) (meta.AbsoluteFilePath, bool) {
	if !isRelativeSpecifier(specifier) {
		return "", false
	}
	base := filepath.Join(filepath.Dir(from), filepath.FromSlash(specifier))

	for _, candidate := range resolveCandidates(base) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// isRelativeSpecifier separates the imports the extractor follows from the bare
// package specifiers it deliberately does not.
func isRelativeSpecifier(specifier string) bool {
	return strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

// defaultExportedFunction locates the file's default export, when that export
// is a function: an `export default function`, an `export default` of a
// function expression, or an `export default` of a locally bound one.
func defaultExportedFunction(file *ast.SourceFile) *ast.Node {
	if file.Statements == nil {
		return nil
	}
	var assigned *ast.Node // `export default X` where X is an identifier
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			// HasSyntacticModifier is any-of, so test the pair directly.
			if stmt.ModifierFlags()&ast.ModifierFlagsExportDefault == ast.ModifierFlagsExportDefault {
				return stmt
			}
		case ast.KindExportAssignment:
			expr := stmt.AsExportAssignment().Expression
			if expr == nil {
				continue
			}
			if isFunctionExpression(expr) {
				return expr
			}
			if expr.Kind == ast.KindIdentifier {
				assigned = expr
			}
		}
	}
	if assigned != nil {
		return boundFunction(file, assigned.Text()) // nil if it is not a function
	}
	return nil
}

func isFunctionExpression(n *ast.Node) bool {
	return n.Kind == ast.KindArrowFunction || n.Kind == ast.KindFunctionExpression
}

// boundFunction finds `const name = <function>` or `function name`.
func boundFunction(file *ast.SourceFile, name string) *ast.Node {
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			if n := stmt.Name(); n != nil && n.Text() == name {
				return stmt
			}
		case ast.KindVariableStatement:
			for _, decl := range variableDeclarations(stmt) {
				n := decl.Name()
				if n == nil || n.Text() != name {
					continue
				}
				if init := decl.Initializer(); init != nil && isFunctionExpression(init) {
					return init
				}
			}
		}
	}
	return nil
}

// variableDeclarations returns the declarations of a `const`/`let`/`var`
// statement, which may bind several names at once.
func variableDeclarations(variableStatement *ast.Node) []*ast.Node {
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

func topLevelType(file *ast.SourceFile, name string) *ast.Node {
	if file.Statements == nil {
		return nil
	}
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			if n := stmt.Name(); n != nil && n.Text() == name {
				return stmt
			}
		}
	}
	return nil
}

// membersToFields keeps property signatures and drops everything else a type
// body may hold — methods, index and call signatures — none of which describe a
// named prop.
func membersToFields(text string, members []*ast.Node) []Field {
	fields := make([]Field, 0, len(members))
	for _, member := range members {
		if member.Kind != ast.KindPropertySignature {
			continue
		}
		name := propertyName(member.Name())
		if name == "" {
			continue
		}
		signature := member.AsPropertySignatureDeclaration()
		ts := "unknown"
		if signature.Type != nil {
			ts = sourceText(text, signature.Type)
		}
		fields = append(fields, Field{
			Name:     name,
			TS:       ts,
			Optional: signature.PostfixToken != nil && signature.PostfixToken.Kind == ast.KindQuestionToken,
		})
	}
	return fields
}

func propertyName(name *ast.Node) string {
	if name == nil {
		return ""
	}
	switch name.Kind {
	case ast.KindIdentifier, ast.KindStringLiteral, ast.KindNumericLiteral:
		return name.Text()
	}
	return ""
}

// sourceText slices a node out of the file. A node's position includes leading
// trivia, which CanonicalTS removes before anything is compared.
func sourceText(text string, node *ast.Node) string {
	start, end := node.Pos(), node.End()
	if start < 0 || end > len(text) || start > end {
		return "unknown"
	}
	return strings.TrimSpace(text[start:end])
}
