package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/hmr"
	"github.com/lilybw/go-solid/shared/logging"
)

/* Confirmed same stack trace
Uncaught ReferenceError: return_tmpl$ is not defined
    at D ((index):13:10664)
    at (index):13:11033
    at (index):13:8293
    at G.l ((index):13:591)
    at y ((index):13:4274)
    at G ((index):13:632)
    at Q ((index):13:8261)
    at (index):13:11027

*/

func main() {
	mux := http.NewServeMux()
	bundler := bundlerOrPanic(mux)
	defer bundler.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received request: %s %s", r.Method, r.URL.Path)
		bundler.Prepare("TopBar", nil).ForRequest(w, r).Render()
	})

	err := http.ListenAndServe(":7490", mux)
	if err != nil {
		fmt.Println("Server failed:", err)
		os.Exit(1)
	}
}

func bundlerOrPanic(mux *http.ServeMux) *go_solid.Bundler {
	var hmrConfig *hmr.HMRConfig
	if mux != nil {
		hmrConfig = &hmr.HMRConfig{
			Mux: mux,
		}
	}
	wd, _ := filepath.Abs(".")
	bundler, err := go_solid.New(&go_solid.Config{
		Components:     filepath.Join(wd, "components"),
		DisableCaching: true,
		HMR:            hmrConfig,
		LogLevel:       logging.LEVEL_TRACE,
	})
	if err != nil {
		panic(err)
	}
	return bundler
}
func bundlerWCFG(cfg *go_solid.Config) *go_solid.Bundler {
	bundler, err := go_solid.New(cfg)
	if err != nil {
		panic(err)
	}
	return bundler
}
