package esbuild

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/lilybw/go-solid-compiler/solid"
	"github.com/lilybw/go-solid-compiler/tsx"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/esbuild"
)

type bundleResult struct {
	JS      []byte
	CSS     []byte
	Sources []meta.AbsoluteFilePath // absolute paths of consumer source files (for invalidation)
}

// SolidPlugin returns an esbuild plugin that lowers Solid JSX in every JSX/TSX
// file before esbuild bundles it. esbuild's own JSX transform is React-shaped
// and cannot produce Solid's template() calls, so the JSX->Solid step must own
// every file in the graph, not just the entry.
//
// The transform runs in-process. esbuild calls OnLoad concurrently across
// files, which is safe here: each call builds its own compiler state.
func SolidPlugin(cfg *BundlerConfig) esbuild.Plugin {
	return esbuild.Plugin{
		Name: "solid-transform",
		Setup: func(build esbuild.PluginBuild) {
			// Intercept .tsx and .jsx (the files containing JSX). Plain .ts and
			// .js need no transform and go straight to esbuild.
			build.OnLoad(esbuild.OnLoadOptions{Filter: `\.[jt]sx$`},
				func(args esbuild.OnLoadArgs) (esbuild.OnLoadResult, error) {
					src, err := os.ReadFile(args.Path)
					if err != nil {
						return esbuild.OnLoadResult{}, err
					}

					file, err := tsx.Parse(args.Path, string(src), tsx.ScriptKindOf(args.Path))
					if err != nil {
						return esbuild.OnLoadResult{}, fmt.Errorf("solid parse %s: %w", args.Path, err)
					}

					contents, err := tsx.TransformSolid(file, solid.Options{})
					if err != nil {
						return esbuild.OnLoadResult{}, fmt.Errorf("solid transform %s: %w", args.Path, err)
					}

					// The transform lowers JSX but leaves type annotations in
					// place, so esbuild strips those. LoaderTS rather than
					// LoaderTSX on purpose: no JSX should remain, and the TS
					// loader fails loudly if any does instead of silently
					// applying the React transform to it.
					return esbuild.OnLoadResult{
						Contents:   &contents,
						Loader:     esbuild.LoaderTS,
						ResolveDir: filepath.Dir(args.Path),
					}, nil
				})
		},
	}
}

func BundleEntry(entryPath string, workspace meta.AbsoluteDirectoryPath, cfg *BundlerConfig) (*bundleResult, error) {
	opts := esbuild.BuildOptions{
		EntryPoints:       []string{entryPath},
		Bundle:            true,
		Write:             false,
		Outdir:            "solidbundle-out",
		Metafile:          true,
		Format:            esbuild.FormatESModule,
		Platform:          esbuild.PlatformBrowser,
		Target:            esbuild.ES2020,
		AbsWorkingDir:     workspace,
		MinifyWhitespace:  cfg.Minify,
		MinifyIdentifiers: cfg.Minify,
		MinifySyntax:      cfg.Minify,
		Sourcemap:         cfg.Sourcemap,
		Plugins:           []esbuild.Plugin{SolidPlugin(cfg)},
		Loader: map[string]esbuild.Loader{
			".css":   esbuild.LoaderCSS,
			".svg":   esbuild.LoaderDataURL,
			".png":   esbuild.LoaderDataURL,
			".woff":  esbuild.LoaderDataURL,
			".woff2": esbuild.LoaderDataURL,
		},
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
	out.Sources = ExtractSourcesFromMetafile(result.Metafile, workspace)
	return out, nil
}

// ExtractSourcesFromMetafile parses esbuild's metafile JSON and returns the
// absolute paths of consumer source files in the bundle graph, excluding
// node_modules and the generated temp entry. These are what invalidation
// hashes.
func ExtractSourcesFromMetafile(metafile string, workspace meta.AbsoluteDirectoryPath) []string {
	if metafile == "" {
		return nil
	}
	var mf struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(metafile), &mf); err != nil {
		return nil
	}
	var srcs []string
	for p := range mf.Inputs {
		// Metafile paths are relative to AbsWorkingDir (workDir).
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workspace, p)
		}
		if strings.Contains(filepath.ToSlash(abs), "/node_modules/") {
			continue
		}
		// Skip the generated temp entry (lives in the workspace .go_solid tree).
		if strings.Contains(filepath.ToSlash(abs), "/.go_solid/") || strings.Contains(filepath.ToSlash(abs), "/.solidbundle-") {
			continue
		}
		srcs = append(srcs, abs)
	}
	sort.Strings(srcs)
	return srcs
}

func WriteTempEntry(workspace meta.AbsoluteDirectoryPath, transformed string) (string, func(), error) {
	dir, err := os.MkdirTemp(workspace, ".solidbundle-*")
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

func NormalizeSourcePath(p string) meta.AbsoluteFilePath {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}
