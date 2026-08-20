package go_solid

import "testing"

// Smoke test: verify the no-Node integration path actually works before the
// rest of the suite depends on it.
func TestSmoke_NewWithDisabledGenerationDoesNotSpawnNode(t *testing.T) {
	resetPackageState(t)
	comps := componentsDirWith(t, map[string]string{
		"Hello.tsx": "export default () => null;",
	})

	// NodeBin set to a path that cannot exist. If New() tried to spawn a worker,
	// exec would fail and New() would return an error. Disabled must short-circuit.
	gen := disabledGeneration()

	b, _ := New(&Config{Components: comps, Generation: gen})
	if b == nil {
		t.Fatal("New() returned nil bundler without error")
	}
	b.Close()
}
