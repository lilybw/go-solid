package internal

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/lilybw/go-solid/internal/esbuild"
)

// norm mirrors what DepIndex does internally to every key, so expected values in
// tests are compared on the same footing as stored ones. Tests must never
// hardcode a raw path as an expected key: NormalizeSourcePath runs filepath.Abs
// + filepath.Clean, whose output is platform- and cwd-dependent.
func norm(p string) string {
	return esbuild.NormalizeSourcePath(p)
}

// sortedDeps returns DependentsOf sorted, since the method builds its result by
// ranging a map and the order is therefore unspecified. Every assertion on
// multi-element results must sort first.
func sortedDeps(d *DependencyIndex, source string) []string {
	got := d.DependentsOf(source)
	sort.Strings(got)
	return got
}

func TestDepIndex_EmptyLookupReturnsNil(t *testing.T) {
	d := NewDepIndex()
	if got := d.DependentsOf("/some/path.tsx"); got != nil {
		t.Fatalf("expected nil for unknown source, got %v", got)
	}
}

func TestDepIndex_SingleComponentSingleSource(t *testing.T) {
	d := NewDepIndex()
	src := filepath.Join("components", "Button.tsx")
	d.Record("ui/Button", []string{src})

	got := sortedDeps(d, src)
	if len(got) != 1 || got[0] != "ui/Button" {
		t.Fatalf("expected [ui/Button], got %v", got)
	}
}

func TestDepIndex_SharedSourceMapsToMultipleComponents(t *testing.T) {
	d := NewDepIndex()
	shared := filepath.Join("components", "shared", "theme.ts")

	d.Record("ui/Button", []string{shared})
	d.Record("ui/Card", []string{shared})
	d.Record("layout/Header", []string{shared})

	got := sortedDeps(d, shared)
	want := []string{"layout/Header", "ui/Button", "ui/Card"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestDepIndex_ComponentWithMultipleSources(t *testing.T) {
	d := NewDepIndex()
	a := filepath.Join("components", "Form.tsx")
	b := filepath.Join("components", "shared", "validators.ts")
	c := filepath.Join("components", "shared", "theme.ts")
	d.Record("ui/Form", []string{a, b, c})

	for _, src := range []string{a, b, c} {
		got := d.DependentsOf(src)
		if len(got) != 1 || got[0] != "ui/Form" {
			t.Fatalf("source %q: expected [ui/Form], got %v", src, got)
		}
	}
}

func TestDepIndex_RecordIsIdempotent(t *testing.T) {
	// Recording the same component+source repeatedly must not create duplicate
	// entries in the set — the value is a set (map[string]struct{}), so this is
	// really asserting the set semantics hold.
	d := NewDepIndex()
	src := filepath.Join("components", "Button.tsx")

	d.Record("ui/Button", []string{src})
	d.Record("ui/Button", []string{src})
	d.Record("ui/Button", []string{src})

	got := d.DependentsOf(src)
	if len(got) != 1 {
		t.Fatalf("expected exactly one entry after repeated Record, got %d: %v", len(got), got)
	}
}

func TestDepIndex_SourceSetGrowsAcrossRenders(t *testing.T) {
	// A component's source set only ever grows within a process lifetime (per the
	// Record doc). A second Record with a superset must union, not replace.
	d := NewDepIndex()
	a := filepath.Join("components", "Form.tsx")
	b := filepath.Join("components", "shared", "validators.ts")

	d.Record("ui/Form", []string{a})
	d.Record("ui/Form", []string{a, b}) // now also depends on b

	if got := d.DependentsOf(a); len(got) != 1 || got[0] != "ui/Form" {
		t.Fatalf("source a: expected [ui/Form], got %v", got)
	}
	if got := d.DependentsOf(b); len(got) != 1 || got[0] != "ui/Form" {
		t.Fatalf("source b: expected [ui/Form] after growth, got %v", got)
	}
}

func TestDepIndex_LookupNormalizesInput(t *testing.T) {
	// A source recorded via one spelling must be found via a different spelling of
	// the same path, because both sides run through NormalizeSourcePath. Using a
	// redundant "." segment is a portable way to produce a differently-spelled but
	// equivalent path on every OS.
	d := NewDepIndex()
	clean := filepath.Join("components", "Button.tsx")
	messy := filepath.Join("components", ".", "Button.tsx")

	d.Record("ui/Button", []string{clean})

	if got := d.DependentsOf(messy); len(got) != 1 || got[0] != "ui/Button" {
		t.Fatalf("expected messy-path lookup to normalize and hit, got %v", got)
	}
}

func TestDepIndex_RecordNormalizesKey(t *testing.T) {
	// Symmetric to the above: record with a messy path, look up with a clean one.
	d := NewDepIndex()
	clean := filepath.Join("components", "shared", "theme.ts")
	messy := filepath.Join("components", "shared", "..", "shared", "theme.ts")

	d.Record("ui/Themed", []string{messy})

	if got := d.DependentsOf(clean); len(got) != 1 || got[0] != "ui/Themed" {
		t.Fatalf("expected clean-path lookup to hit messy-recorded key, got %v", got)
	}
}

func TestDepIndex_EmptySourcesIsNoop(t *testing.T) {
	d := NewDepIndex()
	d.Record("ui/Button", nil)
	d.Record("ui/Button", []string{})
	// Nothing recorded, so no source resolves to it. We can't enumerate keys
	// (no such API), but a fresh unrelated lookup must still be nil.
	if got := d.DependentsOf(filepath.Join("x", "y.tsx")); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDepIndex_DistinctSourcesAreIndependent(t *testing.T) {
	d := NewDepIndex()
	a := filepath.Join("components", "A.tsx")
	b := filepath.Join("components", "B.tsx")
	d.Record("ui/A", []string{a})
	d.Record("ui/B", []string{b})

	if got := d.DependentsOf(a); len(got) != 1 || got[0] != "ui/A" {
		t.Fatalf("source a leaked: got %v", got)
	}
	if got := d.DependentsOf(b); len(got) != 1 || got[0] != "ui/B" {
		t.Fatalf("source b leaked: got %v", got)
	}
}

func TestDepIndex_ReturnedSliceIsSafeToMutate(t *testing.T) {
	// DependentsOf builds a fresh slice each call, so a caller mutating it must
	// not corrupt internal state. (The watcher does not mutate, but this guards
	// the invariant.)
	d := NewDepIndex()
	shared := filepath.Join("components", "shared", "theme.ts")
	d.Record("ui/Button", []string{shared})
	d.Record("ui/Card", []string{shared})

	first := d.DependentsOf(shared)
	if len(first) > 0 {
		first[0] = "MUTATED"
	}
	second := sortedDeps(d, shared)
	want := []string{"ui/Button", "ui/Card"}
	if fmt.Sprint(second) != fmt.Sprint(want) {
		t.Fatalf("internal state corrupted by caller mutation: got %v", second)
	}
}

// --- Concurrency ------------------------------------------------------------

func TestDepIndex_ConcurrentRecordAndLookup(t *testing.T) {
	// Run with -race. Exercises the RWMutex under simultaneous writers and
	// readers touching both shared and distinct keys.
	d := NewDepIndex()
	const workers = 16
	const iters = 200

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for w := 0; w < workers; w++ {
		w := w
		// Writers
		go func() {
			defer wg.Done()
			shared := filepath.Join("components", "shared", "theme.ts")
			for i := 0; i < iters; i++ {
				own := filepath.Join("components", fmt.Sprintf("C%d_%d.tsx", w, i))
				d.Record(fmt.Sprintf("ui/C%d", w), []string{own, shared})
			}
		}()
		// Readers
		go func() {
			defer wg.Done()
			shared := filepath.Join("components", "shared", "theme.ts")
			for i := 0; i < iters; i++ {
				_ = d.DependentsOf(shared)
			}
		}()
	}
	wg.Wait()

	// After all writers, every worker must appear under the shared source.
	got := d.DependentsOf(filepath.Join("components", "shared", "theme.ts"))
	seen := map[string]bool{}
	for _, name := range got {
		seen[name] = true
	}
	for w := 0; w < workers; w++ {
		name := fmt.Sprintf("ui/C%d", w)
		if !seen[name] {
			t.Fatalf("expected %q under shared source after concurrent writes, missing; got %v", name, got)
		}
	}
}

func TestDepIndex_ConcurrentRecordSameKey(t *testing.T) {
	// Many goroutines recording the SAME component+source. Set semantics must
	// leave exactly one entry, and the map init race (set == nil check) must be
	// safe under -race.
	d := NewDepIndex()
	src := filepath.Join("components", "Button.tsx")

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Record("ui/Button", []string{src})
		}()
	}
	wg.Wait()

	if got := d.DependentsOf(src); len(got) != 1 || got[0] != "ui/Button" {
		t.Fatalf("expected single [ui/Button] after concurrent identical records, got %v", got)
	}
}
