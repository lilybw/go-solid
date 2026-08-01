# GO Solid
An integration for Go allowing solidjs to be used for templating. 


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
    DependenciesDir: "./path/to/node_modules",
    ...
})
```

## Registry
This library expects a common folder for components and allows these components to be used as templates in Go using a qualified path from said folder. 

By default, the registry folder is also where the library expects node_modules to be located if not overwritten.
```go
bundler, err := solid.New(solid.Config{
    ...
    ComponentsDir:   "./where/are/your/solid-components",
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

To enable HMR, provide your server ServeMux in the Bundler Config:

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





