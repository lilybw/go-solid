package go_solid

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type bundleResult struct {
	JS  []byte
	CSS []byte
}

// solidPlugin returns an esbuild plugin that intercepts every JSX/TSX source
// file and runs it through the babel-preset-solid worker pool BEFORE esbuild
// bundles it. This is the key to correct output: esbuild's own JSX transform is
// React-shaped and cannot produce Solid's template() calls, so we must let babel
// own the JSX->Solid step for every file in the graph, not just the entry.
func solidPlugin(pool *Pool, generate string) esbuild.Plugin {
	return esbuild.Plugin{
		Name: "solid-transform",
		Setup: func(build esbuild.PluginBuild) {
			// Intercept .tsx and .jsx (the files containing JSX).
			build.OnLoad(esbuild.OnLoadOptions{Filter: `\.[jt]sx$`},
				func(args esbuild.OnLoadArgs) (esbuild.OnLoadResult, error) {
					src, err := os.ReadFile(args.Path)
					if err != nil {
						return esbuild.OnLoadResult{}, err
					}
					transformed, err := pool.Transform(context.Background(), transformRequest{
						Filename: args.Path,
						Code:     string(src),
						Generate: generate,
					})
					if err != nil {
						return esbuild.OnLoadResult{}, fmt.Errorf("solid transform %s: %w", args.Path, err)
					}
					// Loader is TSX so esbuild strips any remaining TS annotations.
					// esbuild's JSX won't fire: babel already removed all JSX.
					contents := transformed
					loader := esbuild.LoaderTSX
					return esbuild.OnLoadResult{
						Contents:   &contents,
						Loader:     loader,
						ResolveDir: filepath.Dir(args.Path),
					}, nil
				})
		},
	}
}

func bundleEntry(pool *Pool, generate, entryPath string, workDir AbsoluteDirectoryPath, minify, dev bool) (*bundleResult, error) {
	opts := esbuild.BuildOptions{
		EntryPoints:       []string{entryPath},
		Bundle:            true,
		Write:             false,
		Outdir:            "solidbundle-out",
		Format:            esbuild.FormatESModule,
		Platform:          esbuild.PlatformBrowser,
		Target:            esbuild.ES2020,
		AbsWorkingDir:     string(workDir),
		MinifyWhitespace:  minify,
		MinifyIdentifiers: minify,
		MinifySyntax:      minify,
		Plugins:           []esbuild.Plugin{solidPlugin(pool, generate)},
		Loader: map[string]esbuild.Loader{
			".css":   esbuild.LoaderCSS,
			".svg":   esbuild.LoaderDataURL,
			".png":   esbuild.LoaderDataURL,
			".woff":  esbuild.LoaderDataURL,
			".woff2": esbuild.LoaderDataURL,
		},
	}
	if dev {
		opts.Sourcemap = esbuild.SourceMapInline
	}

	result := esbuild.Build(opts)
	if len(result.Errors) > 0 {
		var b strings.Builder
		for _, e := range result.Errors {
			loc := ""
			if e.Location != nil {
				loc = fmt.Sprintf(" (%s:%d)", e.Location.File, e.Location.Line)
			}
			b.WriteString(e.Text + loc + "\n")
		}
		return nil, fmt.Errorf("esbuild:\n%s", b.String())
	}

	out := &bundleResult{}
	for _, f := range result.OutputFiles {
		switch strings.ToLower(filepath.Ext(f.Path)) {
		case ".css":
			out.CSS = f.Contents
		case ".js":
			out.JS = f.Contents
		}
	}
	if out.JS == nil {
		return nil, fmt.Errorf("esbuild produced no JS output")
	}
	return out, nil
}

func writeTempEntry(workspace AbsoluteDirectoryPath, transformed string) (string, func(), error) {
	dir, err := os.MkdirTemp(string(workspace), ".solidbundle-*")
	if err != nil {
		return "", nil, err
	}
	entry := filepath.Join(dir, "entry.jsx")
	if err := os.WriteFile(entry, []byte(transformed), 0o644); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	return entry, func() { os.RemoveAll(dir) }, nil
}
