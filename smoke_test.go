package go_solid

import (
	"testing"

	logging "github.com/lilybw/go-solid/shared/logging"
)

// Smoke test: verify the no-bundling integration path actually works before the
// rest of the suite depends on it.
func TestSmoke_NewWithDisabledGenerationSkipsBundling(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})

	// Disabled must short-circuit esbuild and the solid-js resolution gate.
	gen := disabledGeneration()

	b, _ := New(&Config{
		LogLevel:   logging.LEVEL_ERROR,
		Components: comps, Generation: gen})
	if b == nil {
		t.Fatal("New() returned nil bundler without error")
	}
	b.Close()
}
