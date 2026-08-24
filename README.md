# GO Solid 
Modular, native SolidJS templating for Go with HMR and Typesafety.

#### Development Notice
This library uses a "release-if-green" methodology. That means that the readiness of the codebase is only given by how well the testing suites have been written. Thus, always depend on a tagged version, never depend on latest.

## Core 
A brief introduction to the core elements and mechanics of the library.

### Registry
This library expects a common folder for components and allows these components to be used as templates in Go using a qualified path from said folder. 

By default, the registry folder is also where the library expects node_modules to be located if not overwritte (and if required)
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

### Templating
Data is passed to the template using the standard props input for solidjs components. 

In Go, this is expressed as any struct that can be json serialized, for instance a ```map[string]string```:

```go
rendered, err := bundler.Prepare("path/to/Component", map[string]any{"title": "Hello World"}).Render()
```

```ForRequest(writer, request)``` sets status code and content type headers and handles most standard networking automatically
```go
rendered, err := bundler.Prepare("path/to/Component", map[string]any{"title": "Hello World"}).
    ForRequest(writer, request).
    Render()
```

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

### HMR 
go_solid builds a two-way dependency index from esbuilds metafile output when a component is bundled. 
This index is used to, among other things, do module replacement during runtime if such is configured. 

To enable HMR, provide your server's method for adding endpoints in the Bundler Config. Various adapters are already available in the ```compat``` package. 

```go
// MuxLike is anything that can register an http.Handler under a string pattern
type MuxLike interface {
	Handle(pattern string, handler http.Handler)
}
// Same as above but with any return type
type RouterLike[T any] interface {
	Handle(pattern string, handler http.Handler) T
}
```

```go
mux := http.DefaultServeMux();

bundler, err := go_solid.New(&go_solid.Config{
    ...
    HMR:    &hmr.HMRConfig{
        Mux: mux,
    },
})
```
To avoid potential cors issues, do ensure that the WS connections the library will make to any client visiting your servers endpoints, is directed at the same origin as the template the client has been served. (You can set HMR up over another port, but browsers may be perturbed by this)


### Component Selectors & Resolution
Since v1.0.15 go-solid places no constraints on how your components are structured nor what files they are in (as long as they in the directory you've told the registry is your components directory, or a sub-directory thereof). 

Any file in the provided Components dir may declare any amount of exported components. 

Resolution implementation details:

```go
/*
If only a file is named as the QualifiedName to Bundler#Prepare, go-solid will look for a default export.
*/
_, _ = bundler.Prepare("auth/Login", nil).Render()

/*
Any component in a file can be referenced using "#" regardless of how many components are in the file nor whether any default export is present:
*/
_, _ = bundler.Prepare("auth/Login#LoginPage", nil).Render()
_, _ = bundler.Prepare("auth/Login#Signup", nil).Render()
_, _ = bundler.Prepare("auth/Login#UniLogin", nil).Render()

/*
Explicit usage of "#default" resolves to the default export of the file. 
*/ 
_, _ = bundler.Prepare("auth/Login", nil).Render()
// is the same as
_, _ = bundler.Prepare("auth/Login#default", nil).Render()
```

### Workspace
This library utilizes code generation, type caching, component caching... a lot of things that cannot feasibly stay in memory. Thus a workplace is designated. <br/>
By default this location resolves to `<go_solid.Config#Components>/.go-solid`

To direct the library to place the folder somewhere else, set the Workspace field in the config:
```go
bundler, err := solid.New(solid.Config{
    ...
    Workspace: "./somewhere/with/write/access",
    ...
})
```
A new ```.go_solid``` directory will always be created if not present.

## Typesafety
Since v 1.2.0 go-solid introspects the types you have defined for your components and cross-references these definitions with the data provided when you call Prepare(component, props) from your code.

In case of a missing parameter or incompatible type, an error will be raised. This error is surfaced as the result of ```RenderCallBuilder#Render``` or, if a networking request
has been provided (with ```RenderCallBuilder#ForRequest```), as response to said request.

To alter when, or if, these checks should happen, a setting is exposed in the config:

```go
Config{
    Types: &types.TypesConfig{
        Check: CHECK_RUNTIME_AND_BOOT // CHECK_BOOT, CHECK_RUNTIME, CHECK_NEVER
    }
}
```

## Modules
go-solid generates various modules which can be turned on and off through the bundler's config. 

All generated modules can be referenced from: ``` <go_solid.Config#Workspace>/modules```

All possible modules are initially declared and made visible, but not provided further functionality outside of their skeleton. 

### Static 
Since 1.3.0 static asset management have been included as a togglable feature by provided the ```static.StaticConfig```. <br/>
Code generation is used to produce a custom integration from the contents and structure of your static assets folder, alongside a lot of inference.

<i>Security note: <br/>
Static asset management in go-solid does not use any kind of filepath traversal, rather it constructs a map of scanned assets once and checks against this map, leaving no injection possible.</i>

Like HMR, serving assets requires a way to register an endpoint and so Static requires a MuxLike.  

Key settings:
```go
type StaticConfig struct {
    // Parent directory of where your static assets are
	Location meta.AbsoluteDirectoryPath
	// Update the StaticRegistry reactively by watching the aforementioned Location
	Reactive bool
	// Ignore are glob patterns matched against each entry's base name.
	Ignore []FileSelectorPattern
    ...
}
```
To access each asset, Static creates an exported object that is a mirror of your Static asset folder with some name sanitization. <br/>
Take file "logo.svg" in a sub-folder called "images". This graphic will become available as showcased below:
```tsx
import S from "go-solid/static";

export default function Logo() {
  return (
      <img src={S.images.logo} alt="" />
  );
}
```
In case of multiple files with the same name, but different extensions, the object path is extended:
```tsx
<>
    <img src={S.images.logo.PNG} alt="png version" />
    <img src={S.images.logo.SVG} alt="svg version" />
</>
```


## How
Since v1.0.8 this library was moved to lilybw/go-solid-compiler which in turn uses a condensed tsgo fork (lilybw/typescript-go). That means that this library parses and transforms solidjs jsx components natively. 

It is currently not possible to choose what version of solidjs/web to use for templating, as that is bundled with go-solid-compiler.
However various options for what dev/prod variant to use are extended and customizable. 


## Roadmap
Various "plug-in" like features will be made available, accessed as fields on a components props. 

Version 1.4.0 will introduce "navigation", allowing a reduced endpoint repressentaiton be delivered to this library from your code (however you see fit), then formatting that as nothing but fields on the "navigation" props property. 

Note on version numbering: I dont know how to do versioning.

### Adapters
go-solid is rather self-contained and should work with most existing projects. 

One thing that is not, is the HMR implimentation and Bundler#ForRequest, which has to make certain assumptions to work. 

If you find that these does not work for your project, I will gladly accept any PR adding support. 
You may also make an issue, however then it will depend on when I got time to see to it. 
