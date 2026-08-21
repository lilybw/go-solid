package esbuild

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/lilybw/go-solid-compiler/esbuildsolid"
	"github.com/lilybw/go-solid-compiler/runtime"
	"github.com/lilybw/go-solid-compiler/solid"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/esbuild"
)

type bundleResult struct {
	JS      []byte
	CSS     []byte
	Sources []meta.AbsoluteFilePath // absolute paths of consumer source files (for invalidation)
}

// solidPlugins returns the plugins that turn Solid JSX into a browser bundle.
//
// The transform must own every JSX file in the graph, not just the entry:
// esbuild's own JSX support is React-shaped and cannot produce Solid's
// template calls.
//
// Every Solid setting is passed straight through from BundlerConfig#Solid.
// Nothing here is inferred from the bundling options.
func solidPlugins(cfg *BundlerConfig) []esbuild.Plugin {
	return esbuildsolid.Plugins(
		solid.Options{
			ModuleName:        cfg.Solid.ModuleName,
			Prefix:            cfg.Solid.HelperPrefix,
			DisableDelegation: cfg.Solid.DisableEventDelegation,
		},
		runtime.Config{
			Development: cfg.Solid.Development,
			Override:    cfg.Solid.RuntimeOverride,
		},
		cfg.Solid.Runtime == RuntimeInternal,
	)
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
		Plugins:           solidPlugins(cfg),
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
// node_modules, the embedded runtime, and the generated temp entry. These are
// what invalidation hashes.
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
		// Embedded runtime modules appear as "solid-runtime:solid-js/web".
		// They live inside the compiler binary, so there is no file to watch
		// and nothing to invalidate on.
		if strings.HasPrefix(p, "solid-runtime:") {
			continue
		}
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
