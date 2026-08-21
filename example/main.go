package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/esbuild"
	"github.com/lilybw/go-solid/shared/logging"
)

func main() {
	wd, _ := filepath.Abs(".")
	b, err := solid.New(&solid.Config{
		Components: filepath.Join(wd, "components"),
		LogLevel:   logging.LEVEL_ERROR,
		Generation: &esbuild.BundlerConfig{Dependencies: wd},
	})
	if err != nil {
		fmt.Println("New failed:", err)
		os.Exit(1)
	}
	defer b.Close()

	fmt.Println("Registered components:", b.Registry().Names())

	ctx := context.Background()

	// Render LoginForm with props
	t0 := time.Now()
	r, err := b.Prepare("auth/LoginForm", map[string]any{"title": "Hello World"}).WithCtx(ctx).Render()
	if err != nil {
		fmt.Println("Render failed:", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== auth/LoginForm rendered in %v ===\n", time.Since(t0))
	fmt.Printf("JS bytes: %d (name %s)\n", len(r.JS), r.JSName)
	fmt.Printf("CSS bytes: %d (name %s)\n", len(r.CSS), r.CSSName)
	fmt.Println("CSS content:", r.CSS)
	fmt.Println("--- HTML ---")
	fmt.Println(r.HTML)

	// Second render = cache hit
	t1 := time.Now()
	_, _ = b.Prepare("auth/LoginForm", map[string]any{"title": "Hello World"}).WithCtx(ctx).Render()
	fmt.Printf("=== cache hit in %v ===\n", time.Since(t1))

	// Different component, verify tree-shaking gives different size
	r2, _ := b.Prepare("Version", nil).WithCtx(ctx).Render()
	fmt.Printf("\n=== Version: JS bytes: %d ===\n", len(r2.JS))

	// Verify the JS actually contains Solid template calls

	fmt.Println("LoginForm JS contains 'template':", strings.Contains(r.JS, "template"))
}

func init() {
	// dump full JS to a file for inspection when DUMP=1
	if os.Getenv("DUMP") == "" {
		return
	}
	wd, _ := filepath.Abs(".")
	b, err := solid.New(&solid.Config{
		LogLevel:       logging.LEVEL_ERROR,
		Components:     filepath.Join(wd, "components"),
		Generation:     &esbuild.BundlerConfig{Dependencies: wd},
		DisableCaching: true})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer b.Close()
	r, err := b.Prepare("auth/LoginForm", nil).WithCtx(context.Background()).Render()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.WriteFile("/tmp/loginform.js", []byte(r.JS), 0644)
	fmt.Println("dumped", len(r.JS), "bytes to /tmp/loginform.js")
	os.Exit(0)
}
