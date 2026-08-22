package types

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// declarationOf writes source to a real file, because resolution now follows
// imports and therefore needs somewhere to follow them from.
func declarationOf(t *testing.T, filename, source string) Extraction {
	t.Helper()
	return extractionIn(t, t.TempDir(), filename, source)
}

func extractionIn(t *testing.T, dir, filename, source string) Extraction {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	extraction, err := NewExtractor().Component(path)
	if err != nil {
		t.Fatalf("Component(%q): %v", path, err)
	}
	return extraction
}

func assertDeclared(t *testing.T, extraction Extraction, fingerprint string) {
	t.Helper()
	if !extraction.Found {
		t.Fatalf("expected a resolvable props type, got %+v", extraction)
	}
	if got := extraction.Shape.Fingerprint(); got != fingerprint {
		t.Fatalf("shape = %q, want %q", got, fingerprint)
	}
}

// The parser asserts that a file name is absolute and slash-normalized, and
// panics otherwise. A Windows path is neither, and a bare name is not
// absolute, so Parse has to cope with both.
func TestParse_AcceptsOSAndRelativeFileNames(t *testing.T) {
	source := `export default function Hello(props: { title: string }) { return <div/>; }`
	for _, path := range []string{
		`C:\Users\lily\components\Hello.tsx`,
		`E:\GitHub\go-solid\components\auth\LoginForm.tsx`,
		"/home/lily/components/Hello.tsx",
		"Hello.tsx",
		"./nested/Hello.tsx",
		"../sibling/Hello.tsx",
	} {
		tree := Parse(path, source)
		if tree == nil || tree.Statements == nil || len(tree.Statements.Nodes) == 0 {
			t.Errorf("Parse(%q) did not yield a usable tree", path)
		}
	}
}

func TestExtract_InlineTypeLiteral(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
export default function Hello(props: { name?: string; count: number }) {
	return <div>{props.name}</div>;
}
`)
	assertDeclared(t, declaration, "count:number;name?:string")
	if declaration.Name != "" {
		t.Errorf("an inline literal has no name, got %q", declaration.Name)
	}
}

func TestExtract_LocalInterface(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
interface HelloProps {
	name: string;
	tags: string[];
}

export default function Hello(props: HelloProps) {
	return <div>{props.name}</div>;
}
`)
	assertDeclared(t, declaration, "name:string;tags:string[]")
	if declaration.Name != "HelloProps" {
		t.Errorf("Name = %q, want HelloProps", declaration.Name)
	}
}

func TestExtract_LocalTypeAlias(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
type Props = { id: string };
export default function Hello(props: Props) { return <div/>; }
`)
	assertDeclared(t, declaration, "id:string")
}

func TestExtract_Intersection(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
interface A { a: string }
type B = { b: number };
export default function Hello(props: A & B) { return <div/>; }
`)
	assertDeclared(t, declaration, "a:string;b:number")
}

func TestExtract_ArrowExportedAsDefault(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
const Hello = (props: { title: string }) => <h1>{props.title}</h1>;
export default Hello;
`)
	assertDeclared(t, declaration, "title:string")
}

func TestExtract_AnonymousDefaultArrow(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
export default (props: { title: string }) => <h1>{props.title}</h1>;
`)
	assertDeclared(t, declaration, "title:string")
}

func TestExtract_SoleFunctionWithoutDefaultExport(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
export function Hello(props: { title: string }) { return <h1/>; }
`)
	assertDeclared(t, declaration, "title:string")
}

// `export default function` must win over any other function in the file.
func TestExtract_PrefersTheDefaultExport(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
function helper(props: { wrong: string }) { return null; }
export default function Hello(props: { right: string }) { return <div/>; }
`)
	assertDeclared(t, declaration, "right:string")
}

func TestExtract_MembersThatAreNotPropsAreIgnored(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `
interface Props {
	title: string;
	onClick(): void;
	[key: string]: unknown;
}
export default function Hello(props: Props) { return <div/>; }
`)
	assertDeclared(t, declaration, "title:string")
}

func TestExtract_FormattingDoesNotChangeTheShape(t *testing.T) {
	spread := declarationOf(t, "A.tsx", `
export default function A(props: {
	// the caption
	title:    string,
	count ?: number
}) { return <div/>; }
`)
	tight := declarationOf(t, "B.tsx", `
export default function B(props: {title: string; count?: number}) { return <div/>; }
`)
	if !spread.Found || !tight.Found {
		t.Fatal("both declarations should resolve")
	}
	if !spread.Shape.Equal(tight.Shape) {
		t.Fatalf("formatting changed the shape:\n%q\n%q",
			spread.Shape.Fingerprint(), tight.Shape.Fingerprint())
	}
}

func TestExtract_NoParameter(t *testing.T) {
	declaration := declarationOf(t, "Hello.tsx", `export default function Hello() { return <div/>; }`)
	if declaration.Found {
		t.Error("a component without a parameter declares no props")
	}
	if declaration.HasParameter {
		t.Error("HasParameter should be false")
	}
}

func TestExtract_UntypedParameter(t *testing.T) {
	declaration := declarationOf(t, "Hello.jsx", `export default function Hello(props) { return <div/>; }`)
	if declaration.Found {
		t.Error("an unannotated parameter declares no props")
	}
	if !declaration.HasParameter {
		t.Error("HasParameter should be true: there is a parameter, just no type")
	}
}

func TestExtract_FollowsARelativeTypeImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shared.d.ts"),
		[]byte("export interface Props { id: string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extraction := extractionIn(t, dir, "Hello.tsx", `
import type { Props } from "./shared";
export default function Hello(props: Props) { return <div/>; }
`)
	assertDeclared(t, extraction, "id:string")
	if len(extraction.Sources) != 2 {
		t.Fatalf("Sources = %v, want the component and the file it imported", extraction.Sources)
	}
}

func TestExtract_FollowsARenamedImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nav.d.ts"),
		[]byte("export interface Navigation { path: string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extraction := extractionIn(t, dir, "Hello.tsx", `
import type { Navigation as Nav } from "./nav";
export default function Hello(props: Nav) { return <div/>; }
`)
	assertDeclared(t, extraction, "path:string")
}

// The pattern the published surface exists for: a component intersects its own
// props with a definition go_solid synthesised.
func TestExtract_IntersectsLocalPropsWithAnImportedType(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "types"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types", "navigation.d.ts"),
		[]byte("export interface Navigation { currentPath: string; back?: string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extraction := extractionIn(t, dir, "pages/Home.tsx", `
import type { Navigation } from "../types/navigation";
export default function Home(props: { title: string } & Navigation) { return <div/>; }
`)
	assertDeclared(t, extraction, "back?:string;currentPath:string;title:string")
	if len(extraction.Unresolved) != 0 {
		t.Errorf("nothing should be unresolved, got %v", extraction.Unresolved)
	}
}

// An arm that cannot be followed must not take the resolvable ones with it:
// under-checking is safe, abandoning the component silently is not.
func TestExtract_KeepsResolvableArmsOfAnIntersection(t *testing.T) {
	extraction := declarationOf(t, "Hello.tsx", `
import type { Mystery } from "some-package";
export default function Hello(props: { title: string } & Mystery) { return <div/>; }
`)
	assertDeclared(t, extraction, "title:string")
	if !slices.Contains(extraction.Unresolved, "Mystery") {
		t.Errorf("Unresolved = %v, want Mystery named", extraction.Unresolved)
	}
}

// A bare specifier is not followed, so nothing walks into node_modules.
func TestExtract_BarePackageImportIsNotFollowed(t *testing.T) {
	extraction := declarationOf(t, "Hello.tsx", `
import type { Props } from "solid-js";
export default function Hello(props: Props) { return <div/>; }
`)
	if extraction.Found {
		t.Error("a bare package specifier should not resolve")
	}
	if !extraction.HasParameter {
		t.Error("HasParameter should still be true")
	}
}

func TestExtract_ImportCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.d.ts"),
		[]byte("import type { B } from \"./b\";\nexport type A = B;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.d.ts"),
		[]byte("import type { A } from \"./a\";\nexport type B = A;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extraction := extractionIn(t, dir, "Hello.tsx", `
import type { A } from "./a";
export default function Hello(props: A) { return <div/>; }
`)
	if extraction.Found {
		t.Error("a cycle resolves to nothing")
	}
}

func TestExtract_SelfReferentialAliasTerminates(t *testing.T) {
	// Nonsense TypeScript, but it must not spin.
	declaration := declarationOf(t, "Hello.tsx", `
type Props = Props;
export default function Hello(props: Props) { return <div/>; }
`)
	if declaration.Found {
		t.Error("a self-referential alias resolves to nothing")
	}
}
