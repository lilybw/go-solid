# GO Solid 
Native SolidJS templating for Go with HMR.

#### Maturity
Core API is defined and not likely to change significantly. 

Tagged versions are available.

This library uses a "release-if-green" methodology. That means that the readiness of the codebase is only given by how well the testing suites have been written. Thus, always depend on a tagged version, never depend on latest.

## How
Since 1.0.8 this library was moved to lilybw/go-solid-compiler which in turn uses a condensed tsgo fork (lilybw/typescript-go). That means that this library parses and transforms solidjs jsx components natively. 

It is currently not possible to choose what version of solidjs/web to use for templating, as that is bundled with go-solid-compiler.
However various options for what dev/prod variant to use are extended and customizable. 

#### Roadmap
Ever since the introduction of tsgo, it is now possible to do rather sophisticated typegen and introspection. Likewise with the move to go 1.27 it is now possible to define rather sophisticated apis. 

In v1.2.0 go-solid will introduce generated types and data validation in development to assure typesafety and ease of debugging. 

From hereon, various "plug-in" like features will be made available, accessed as fields on a components props. 

The already hinted-at "static" feature will introduce easy, yet secure, management and retrieval of static assets but relies on the former and has as such been slightly postponed. 

Version 1.3.0 will introduce "navigation", allowing a reduced endpoint repressentaiton be delivered to this library from your code (however you see fit), then formatting that as nothing but fields on the "navigation" props property. 

Note on version numbering: I dont know how to do versioning.

### Caching
To try and remain performant, the library uses a mem cache, but also writes bundled and parsed components to disk. 

Caching can be configured in the Config and likewise can the location for the cached js, html, and css files and metafiles be set or overwritten there. 

Given that this library does interact with your filesystem, and potentially project structure, directly, testing suites are run in docker on both linux and windows. More testing environments can be added.

To direct the library where to place its cache, set the Workspace field in the Config:

```go
bundler, err := solid.New(solid.Config{
    ...
    Workspace: "./somewhere/with/write/access",
    ...
})
```
A new ```.go_solid``` directory will always be created in said workspace.

## Registry
This library expects a common folder for components and allows these components to be used as templates in Go using a qualified path from said folder. 

By default, the registry folder is also where the library expects node_modules to be located if not overwritten.
```go
bundler, err := solid.New(solid.Config{
    ...
    Components:   "./where/are/your/solid-components",
    ...
})
```
A given template ```auth/LoginForm.tsx``` are then referenced from the components dir as such:
```go
renderedTemplate, err := bundler.Prepare("auth/LoginForm", props).Render()
```

## Templating Data
Data is passed to the template using the standard props input for solidjs components. 

In Go, this is expressed as any struct that can be json serialized, for instance a ```map[string]any```:

```go
rendered, err := bundler.Prepare("path/to/Component", map[string]any{"title": "Hello World"}).Render()
```

## HMR 
go_solid builds a two-way dependency index from esbuilds metafile output when a component is bundled. 
This index is used to, among other things, do hot module replacement if such is configured. 

To enable HMR, provide your server's ServeMux in the Bundler Config:

```go
mux := http.DefaultServeMux();
bundler, err := go_solid.New(solid.New(solid.Config{
    ...
    HMR:    go_solid.HMRConfig{
        Mux: mux,
    },
})
```
To avoid potential cors issues, do ensure that the WS connections the library will make to any client visiting your servers endpoints, is directed at the same origin as the template the client has been served. (You can set HMR up over another port, but browsers may be perturbed by this)

## Serving Templates
This library has direct integration with Go's standard http package and will handle most things automatically when used in combination:

```go
mux := http.NewServeMux()

mux.HandleFunc("/<route>", func(w http.ResponseWriter, r *http.Request) {
  _, serverErr := bundler.Prepare("TestServer", props).ForRequest(w, r).Render()
})

http.ListenAndServe(":<port>", mux)
```

```ForRequest(writer, request)``` sets status code and content type headers.

All networking behaviour can be altered on a per bundler#Prepare basis using ```SetHTTPBehaviour(configurator)```:

```go
bundler.Prepare("ComponentName", props).
		ForRequest(writer, request).
		SetHTTPBehaviour(func(builder networking.RequestBehaviourBuilder) {
			builder.TransmitRenderedTemplate(/*...custom handler...*/).
				UponPropsMarshalingError(/*...custom handler...*/).
				UponRegistryReloadError(/*...custom handler...*/).
                SetSuccessCode(201 /*created*/)
		}).
		Render()
```

### Adapters
If you would like for go_solid to work for your server framework/library of choice, I see no issue in introducing multiple variants of ForRequest.

I.e. ForFiberRequest, ForGinRequest... etc. 
