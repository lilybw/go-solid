module github.com/lilybw/go-solid/testing_environment/scenarios/shim-g-middleware

go 1.27.0

replace github.com/lilybw/go-solid => ../../..

replace golang.org/x/sys => github.com/golang/sys v0.28.0

require (
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/sessions v1.4.0
	github.com/lilybw/go-solid v1.2.13
)

require (
	github.com/coder/websocket v1.8.15 // indirect
	github.com/evanw/esbuild v0.28.2 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/lilybw/go-solid-compiler v0.2.3 // indirect
	github.com/lilybw/typescript-go v0.1.0 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)
