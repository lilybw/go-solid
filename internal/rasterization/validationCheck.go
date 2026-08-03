package rasterization

import (
	"fmt"
	"os"
	"path/filepath"

	caching_int "github.com/lilybw/go-solid/internal/caching"
	"github.com/lilybw/go-solid/internal/meta"
	. "github.com/lilybw/go-solid/shared/rasterization"
)

func ExpectCompletedValidationCheck(cfg *RasterizationConfig) error {
	// Named directory should contain:
	// transform-worker.*.js

	if _ /*file*/, found, err := FindFirstFileByPattern(cfg.Location, "transform-worker.*.mjs"); err != nil {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, error occurred while searching: %w", cfg.Location, err)
	} else if !found {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, does not contain transform-worker.*.js", cfg.Location)
	}

	// sub-dir: component_cache

	if componentCacheDir, err := os.Stat(filepath.Join(cfg.Location, caching_int.CACHE_DIR_NAME)); err != nil {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, does not contain component_cache dir: %w", cfg.Location, err)
	} else if !componentCacheDir.IsDir() {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, component_cache is not a directory", cfg.Location)
	}

	// component_cache should contain:

	// at least 1 *.meta.json file
	if manifestFile, found, err := FindFirstFileByPattern(filepath.Join(cfg.Location, caching_int.CACHE_DIR_NAME), "*.meta.json"); err != nil {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, error occurred while searching for *.meta.json: %w", cfg.Location, err)
	} else if !found {
		return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, does not contain cached components (no *.meta.json files)", cfg.Location)
	} else {
		if manifest, err := caching_int.ReadManifest(filepath.Join(cfg.Location, caching_int.CACHE_DIR_NAME, manifestFile.Name())); err != nil {
			return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, error occurred while reading manifest %s: %w", cfg.Location, manifestFile.Name(), err)
		} else {
			if err := manifest.Validate(); err != nil {
				return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, manifest %s is invalid: %w", cfg.Location, manifestFile.Name(), err)
			}
			// all mentioned *.meta.json#artifacts should exist within same dir
			artifactsDir := filepath.Join(cfg.Location, caching_int.CACHE_DIR_NAME)
			if _, err := os.Stat(filepath.Join(artifactsDir, manifest.Artifacts.HTML)); err != nil {
				return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, manifest %s references missing HTML artifact %s: %w", cfg.Location, manifestFile.Name(), manifest.Artifacts.HTML, err)
			}
			if _, err := os.Stat(filepath.Join(artifactsDir, manifest.Artifacts.JS)); err != nil {
				return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, manifest %s references missing JS artifact %s: %w", cfg.Location, manifestFile.Name(), manifest.Artifacts.JS, err)
			}
			if _, err := os.Stat(filepath.Join(artifactsDir, manifest.Artifacts.CSS)); manifest.Artifacts.CSS != "" && err != nil {
				return fmt.Errorf("go_solid: RasterizationConfig is invalid, specified location; %q, manifest %s references missing CSS artifact %s: %w", cfg.Location, manifestFile.Name(), manifest.Artifacts.CSS, err)
			}
		}
	}

	return nil
}

func FindFirstFileByPattern(base meta.AbsoluteDirectoryPath, pattern string) (os.DirEntry, bool, error) {
	files, err := os.ReadDir(string(base))
	if err != nil {
		return nil, false, err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if matched, err := filepath.Match(pattern, f.Name()); err == nil && matched {
			return f, true, nil
		}
	}

	return nil, false, nil
}

func FindFirstFileByPatternRecursive(base meta.AbsoluteDirectoryPath, pattern string) (os.DirEntry, bool, error) {
	found := false
	var file os.DirEntry
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if matched, _ := filepath.Match(pattern, d.Name()); matched {
				found = true
				file = d
				return filepath.SkipAll
			}
		}
		return nil
	})
	return file, found, err
}
