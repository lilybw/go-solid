# solidbundle

Adaptive SolidJS component bundling for a Go backend. A Go server names a
component (by its path in a components folder); solidbundle compiles it to a
self-contained HTML + JS + CSS bundle on demand, caches it, and serves it.

## Architecture

    name + props
      -> registry lookup (folder-derived: components/auth/LoginForm.tsx = "auth/LoginForm")
      -> generate entry module (mounts <Component/>, reads props from data island)
      -> esbuild (Go, in-process) bundles the graph, and for every .tsx/.jsx file
         an OnLoad plugin runs babel-preset-solid via a persistent Node worker pool
      -> esbuild typestrips, tree-shakes, collects CSS, minifies
      -> assemble index.html referencing hashed JS + CSS
      -> cache (disabled in Dev mode)

Node is required ONLY for the babel JSX->Solid transform, and only at
boot / cache-miss. esbuild itself runs in-process in Go with no Node. Solid's
compiler is JavaScript (babel-preset-solid / dom-expressions); there is no
Go port, so a Node worker is unavoidable if you want real Solid output (compiled
template() calls) rather than slow runtime hyperscript.

## Why the worker pool

The transform must run per JSX file in the dependency graph. Workers are
persistent (babel imported once, stays warm) and pooled. PoolSize defaults to 1
but is configurable; the calling code is identical at any size.

## Usage

    b, err := solidbundle.New(solidbundle.Config{
        ComponentsDir: "frontend/components",
        WorkDir:       "frontend",            // must resolve solid-js + babel-preset-solid
        WorkerScript:  "internal/worker/transform-worker.mjs",
        PoolSize:      1,
        Minify:        true,
    })
    defer b.Close()

    rendered, err := b.Render(ctx, "auth/LoginForm", map[string]any{"title": "Sign in"})
    // rendered.HTML  -> serve to client
    // rendered.JS / rendered.CSS with rendered.JSName / rendered.CSSName
    //   -> write to your static dir under /static/dist/

Wire Render into your handler where a template name is currently referenced.
Precompute at boot by iterating b.Registry().Names() and calling Render for each.

## Node dependencies (in WorkDir)

    npm install solid-js babel-preset-solid @babel/core

## Go module note

esbuild pulls golang.org/x/sys transitively. If your network blocks
golang.org, add to go.mod:

    replace golang.org/x/sys => github.com/golang/sys@v0.28.0

## Verified

The included example compiles auth/LoginForm to genuine Solid output
(template() calls, fine-grained createRenderEffect updates, delegated events),
splits CSS into a hashed file, and serves cache hits in ~13µs vs ~360ms cold.

## Status / caveats

- This is a working core, verified end-to-end in a Linux container with Go 1.22,
  Node, esbuild v0.24.2, solid-js 1.9, babel-preset-solid 1.9.
- Not yet added: file-watch driven registry reload, worker crash respawn
  supervision, request coalescing (two concurrent misses for the same key both
  build), and SSR (generate:"ssr" is plumbed but the HTML shell renders client-only).
- The worker drains stderr to Discard; wire it to your logger in production.
