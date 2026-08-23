package types

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/lilybw/go-solid/internal/meta"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/ast"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/core"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/parser"
	"github.com/lilybw/typescript-go/use-at-your-own-risk/tspath"
)

// Extraction is what a component file says about its props.
//
// The component is the source of truth: whatever it declares is the contract
// the Go side must satisfy. Sources lists every file that had to be read to
// work the shape out, which is what makes the result cacheable — an entry is
// stale as soon as any of them changes.
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
// Resolution is syntactic — the parser only, no checker and no program — and
// covers inline type literals, local interfaces and aliases, intersections of
// those, and named type imports through relative specifiers. Bare package
// specifiers are not followed, which also keeps it out of node_modules.
//
// Parsed files are held between extractions and re-read when their size or
// modification time changes, so a tree of components importing one generated
// definition parses it once.
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
func (e *Extractor) Component(path meta.AbsoluteFilePath) (Extraction, error) {
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
//
// The file name is only an identity label on the tree, but the parser asserts
// that it is absolute and slash-normalized and panics otherwise — which an OS
// path on Windows is not. Normalizing here means no caller has to know that.
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

// maxTypeResolutionDepth bounds alias and import chasing.
const maxTypeResolutionDepth = 8

// resolver carries the state of one extraction: the files it has touched, the
// names it could not follow, and a guard against a type that refers to itself.
//
// visited holds the names on the current resolution path, not every name the
// extraction has ever seen. The distinction matters for a diamond — two arms of
// an intersection reaching the same type — which is not a cycle and must
// resolve on both arms.
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
			merged = append(merged, shape.Fields...)
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
			target, ok := resolveModule(f.path, decl.ModuleSpecifier.Text())
			if !ok {
				return "", "", false
			}
			return target, exported, true
		}
	}
	return "", "", false
}

// moduleExtensions are tried in TypeScript's order for a specifier that names
// no extension of its own.
var moduleExtensions = []string{".ts", ".tsx", ".d.ts"}

// resolveModule turns a relative module specifier into a file. Bare specifiers
// are left alone: resolving them needs the node resolution algorithm and would
// walk into node_modules, which holds nothing go_solid generated.
func resolveModule(from meta.AbsoluteFilePath, specifier string) (meta.AbsoluteFilePath, bool) {
	if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
		return "", false
	}
	base := filepath.Join(filepath.Dir(from), filepath.FromSlash(specifier))

	candidates := make([]string, 0, 2*len(moduleExtensions)+1)
	if ext := filepath.Ext(base); ext != "" {
		candidates = append(candidates, base)
	}
	for _, ext := range moduleExtensions {
		candidates = append(candidates, base+ext)
	}
	for _, ext := range moduleExtensions {
		candidates = append(candidates, filepath.Join(base, "index"+ext))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// defaultExportedFunction locates the component: an exported default function
// declaration, an `export default` of a function or of a locally bound one, or
// failing that the sole function declared at the top level.
func defaultExportedFunction(file *ast.SourceFile) *ast.Node {
	if file.Statements == nil {
		return nil
	}
	var (
		assigned  *ast.Node // `export default X` where X is an identifier
		candidate *ast.Node // last top-level function seen
		count     int
	)
	for _, stmt := range file.Statements.Nodes {
		switch stmt.Kind {
		case ast.KindFunctionDeclaration:
			count++
			candidate = stmt
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
		if fn := boundFunction(file, assigned.Text()); fn != nil {
			return fn
		}
	}
	if count == 1 {
		return candidate
	}
	if fn := soleBoundFunction(file); fn != nil {
		return fn
	}
	return candidate
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

// soleBoundFunction returns the only function expression bound at the top
// level, if there is exactly one.
func soleBoundFunction(file *ast.SourceFile) *ast.Node {
	var found *ast.Node
	count := 0
	for _, stmt := range file.Statements.Nodes {
		if stmt.Kind != ast.KindVariableStatement {
			continue
		}
		for _, decl := range variableDeclarations(stmt) {
			if init := decl.Initializer(); init != nil && isFunctionExpression(init) {
				found = init
				count++
			}
		}
	}
	if count == 1 {
		return found
	}
	return nil
}

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
