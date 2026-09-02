package go_solid

import (
	code_gen "github.com/lilybw/go-solid/internal/code-gen"
	"github.com/lilybw/go-solid/internal/sources"
	"github.com/lilybw/go-solid/shared/meta"
)

// ANONYMOUS_DIR is the name inline components are grouped under. It is part of
// their qualified name, not a directory: nothing is written for them.
const ANONYMOUS_DIR = "anonymous"

const anonymousExt = ".tsx"

// Anonymous holds a component written inline and prepares a render of it. The
// source may be a declaration, a bare function, or bare JSX, with any imports
// and helpers it needs written before it.
//
//	bundler.Anonymous(`(props) => <p>{props.msg}</p>`, Greeting{Msg: "hi"}).Render()
//	bundler.Anonymous(`<p>hi</p>`, meta.NIL_PROPS).Render()
//
// The component is named after its own text and kept in memory, so an unchanged
// source renders from cache, a changed one is a different component, and no
// bundler — ephemeral or not — writes anything down for it.
func (this *Bundler) Anonymous(source string, props any) RenderCallBuilder {
	component, err := this.holdAnonymous(source)
	if err != nil {
		return newRenderCallBuilder(this, component, props, renderFaults{source: err})
	}
	return this.Prepare(component, props)
}

// holdAnonymous normalizes the fragment, holds it in memory and registers it,
// returning the selector naming the export it declares.
func (this *Bundler) holdAnonymous(source string) (meta.QualifiedName, error) {
	fragment, err := code_gen.NormalizeAnonymous(source)
	if err != nil {
		return "", err
	}

	file := ANONYMOUS_DIR + "/" + fragment.Export
	path := sources.MemoryPath(file + anonymousExt)
	this.held.Put(path, fragment.Module)
	if err := this.registry.Adopt(file, path, anonymousExt); err != nil {
		return "", err
	}
	return meta.JoinSelector(file, fragment.Export), nil
}
