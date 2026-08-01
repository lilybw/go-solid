# GO Solid 
SolidJS templating for Go with HMR. 

Also this readme uses the words "template" and "component" (solidjs component) interchangably.

## Requirements
This library uses esbuild-go to generate templates. However, esbuild-go only supports React inherently. 

Thus the need for node workers, and by extension a couple of node modules, which this library uses to apply the solidjs transform. 

To provide these dependencies, include this package.json in your project and run ```npm install```. This also allows you to control what versions of the dependencies you would like to use:
```json
{
  "name": "stub-for-dependencies",
  "version": "1.0.0",
  "description": "",
  "main": "index.js",
  "scripts": {
    "test": "echo \"Error: no test specified\" && exit 1"
  },
  "keywords": [],
  "author": "",
  "license": "ISC",
  "dependencies": {
    "@babel/core": "^7.29.7",
    "babel-preset-solid": "^1.9.12",
    "solid-js": "^1.9.14"
  }
}
```
Then, when configuring the library, direct it to where to find said dependencies. The location defaults to the registry directory. 

```go
bundler, err := solid.New(solid.Config{
    ...
    Dependencies: "./path/to/node_modules",
    ...
})
```


### Caching
To try and remain performant, the library uses a mem cache, but also writes bundled and parsed components to disk. 

Caching can be configured in the Config and likewise can the location for the cached js, html, and css files and metafiles be set or overwritten there. 

Given that this library does interact with your filesystem, and potentially project structure, directly, only windows has been verified to work reliably. Linux should work too, but please report and bugs you find. 

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

```ForRequest(writer, request)``` sets status code headers

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

## On the Subject of Node
I would love to remove the node dependency and worker pool as it introduces a lot of possible points of failure (not to mention cross-process performance overhead). 

However, it doesnt appear feasible until someone ports first the babel abstract syntax tree, then the solidjs transform and compiler to Go. 

So in the meantime I am working on a way, to enable a setting that disables all node workers and thus only pre-cached components can be called upon. By disabling workers, node is no longer called upon, however no components can be rebundled. 
Not ideal for development, but given that the cache is configurable, it should be able to be copied over during any deployment.