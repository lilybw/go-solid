# shim_a — a vertical-slice test case for go-solid

This is an isolated, self-contained cutout of how a real application. It exists so that the
end-to-end render path — Go registry → esbuild → node/`babel-preset-solid`
transform → HTML assembly — is exercised by go-solid's own test suite, using
the same call shapes a real consumer uses.

The original app is large (gorilla/mux routing, sessions, TLS, a user-service
integration, chunked uploads, a geoserver proxy). None of that touches the
templating library, so it is all dropped. Only two integration points remain:

1. **Construction** — mirrors `services/appState.go → InitAppState`:

   ```go
   go_solid.New(&go_solid.Config{
       Components: filepath.Join(..., "frontend", "components"),
       Defaults:   &go_solid.BehaviouralDefaults{
           HeadSegment: func(b networking.HTMLHeadSegmentBuilder) { b.SetTitle("HOTS") },
       },
       // HMR / Static / ReactiveRegistry are dev-mode only in the original and omitted here.
   })
   ```

2. **Rendering** — mirrors `api/uiRoutes.go` and `api/endpoint.go`:

   ```go
   bundler.Prepare(name, props).ForRequest(w, r).Render()
   ```

## Layout

```
shim_a/
├── slice_test.go                          # the test package
├── README.md
├── .gitignore                             # ignores the .go_solid workspace dir
└── testdata/
    └── frontend/
        ├── package.json                   # pins the SolidJS peer deps
        ├── package-lock.json
        ├── .gitignore                     # ignores node_modules/
        └── components/
            ├── Home.jsx                   # top-level component  -> registers as "Home"
            └── auth/
                └── LoginForm.tsx          # nested component     -> registers as "auth/LoginForm"
```

## Prerequisites

The render tests run the real transform pipeline, so they need:

- **Node.js** on `PATH` (go-solid spawns `node` transform workers).
- The **SolidJS peer deps** installed under `testdata/frontend/node_modules`
  (`solid-js`, `babel-preset-solid`, `@babel/core`). go-solid's `New()`
  resolves these from the components dir upward, so installing them in
  `testdata/frontend/` (an ancestor of `components/`) is sufficient.

Install once:

```sh
cd testdata/frontend && npm ci
```

If Node or the peer deps are absent, the pipeline tests **skip** (they do not
fail), so `go test ./...` stays green on machines without the JS toolchain.

## Wiring it into go-solid's suite

Two options, pick whichever matches how go-solid organizes tests:

### Option A — in-repo package (simplest)

Drop this directory at `testcases/shim_a/` inside the go-solid repo and
**delete** any `go.mod`/`go.sum` from it. It then shares go-solid's own module,
imports `github.com/lilybw/go-solid` directly, and needs no `replace`. It runs
as part of `go test ./...` from the repo root.

### Option B — standalone module

Keep it as its own module. Add a `go.mod` like:

```
module github.com/lilybw/go-solid/testcases/shim_a

go 1.22

require github.com/lilybw/go-solid v0.0.0
replace github.com/lilybw/go-solid => ../..   // point at the go-solid checkout
```

Use this if you want the test case to carry its own dependency closure
(e.g. run against a tagged go-solid release rather than the working tree).

## What the tests cover

| Test | Mirrors | Asserts |
|------|---------|---------|
| `TestRegistryDiscoversComponents` | registry population at `New()` | `Home` and `auth/LoginForm` are discovered by qualified name |
| `TestRenderHomeForRequest` | `api/uiRoutes.go#HomePage` | full render succeeds, `<title>HOTS</title>` from the head-segment default is present, HTTP 200 |
| `TestRenderQualifiedLoginForm` | `api/endpoint.go#requireSession` (working form) | rendering the **qualified** name `auth/LoginForm` succeeds |
| `TestUnqualifiedLoginFormFails` | `api/endpoint.go#requireSession` (the bug) | rendering the **bare** name `LoginForm` fails with `no component registered`, and the error surfaces the qualified name that would have worked |

## The bug this slice captures

The original app calls `Prepare("LoginForm", ...)` with a **bare** component
name. go-solid registers components by their **path-relative qualified name**,
so a component at `frontend/components/auth/LoginForm.tsx` is registered as
`auth/LoginForm`, not `LoginForm`. The bare-name render therefore fails:

```
go_solid#Render: no component registered as "LoginForm" (have: Home, auth/LoginForm)
```

`TestUnqualifiedLoginFormFails` pins this behaviour. The fix on the application
side is to render the qualified name (`auth/LoginForm`) or to move the component
to the top level of the components dir. If go-solid instead decides to support
basename-fallback resolution, that test is the one to update.

> Note: the head-segment default (`SetTitle("HOTS")`) applies globally via
> `SetHTMLHeadSegmentTemplate`, so if go-solid's test suite runs other cases in
> the same process that also set head defaults, be aware of that shared state.
