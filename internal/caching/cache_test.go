package caching

import (
	"fmt"
	"sync"
	"testing"
)

func Test_Deterministic(t *testing.T) {
	a := NewBuildCacheKey("auth/LoginForm", "app-root", "build-1")
	b := NewBuildCacheKey("auth/LoginForm", "app-root", "build-1")
	if a.String() != b.String() {
		t.Errorf("caching.CacheKey not deterministic: %q != %q", a, b)
	}
}

// Every field is part of the identity the disk cache matches on. Comparing the
// *pointers* here would compare two fresh allocations and pass whatever String
// does, so the assertion has to go through String.
func Test_Key_SensitiveToEachInput(t *testing.T) {
	base := NewBuildCacheKey("comp", "root", "build")

	cases := map[string]*CacheKey{
		"different component": NewBuildCacheKey("other", "root", "build"),
		"different root":      NewBuildCacheKey("comp", "other-root", "build"),
		"different build":     NewBuildCacheKey("comp", "root", "other-build"),
	}
	for label, got := range cases {
		if got.String() == base.String() {
			t.Errorf("caching.CacheKey collision for %s: key did not change", label)
		}
	}
}

// The mount root is not part of what a bundle contains, but it is part of what
// an entry is: the shell that ships with the artifact names it. Two roots must
// not share one disk entry.
func Test_Key_RootReachesTheDiskIdentity(t *testing.T) {
	a := NewMemCacheKey("comp", "root-a")
	b := NewMemCacheKey("comp", "root-b")
	if a.String() == b.String() {
		t.Error("CacheKey.String ignores Root; the disk cache cannot tell two mount roots apart")
	}
	if entryStem(a) == entryStem(b) {
		t.Error("entryStem collapses two roots onto one entry; Put would overwrite")
	}
}

// The length prefixes matter: without them, component="ab"+root="c" would
// digest identically to component="a"+root="bc".
func Test_cacheKey_NoConcatenationCollision(t *testing.T) {
	for _, pair := range [][2]*CacheKey{
		{NewMemCacheKey("ab", "c"), NewMemCacheKey("a", "bc")},
		{NewBuildCacheKey("a", "b", "cd"), NewBuildCacheKey("a", "bc", "d")},
	} {
		if pair[0].String() == pair[1].String() {
			t.Errorf("CacheKey collides across a field boundary: %+v vs %+v", *pair[0], *pair[1])
		}
	}
}

func TestShortHash_LengthAndStability(t *testing.T) {
	h1 := ShortHash("hello world", 8)
	h2 := ShortHash("hello world", 8)
	if h1 != h2 {
		t.Errorf("shortHash not stable: %q != %q", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("shortHash len = %d, want 8", len(h1))
	}
}

func TestShortHash_DifferentInputsDiffer(t *testing.T) {
	if ShortHash("a", 8) == ShortHash("b", 8) {
		t.Error("shortHash produced same prefix for different inputs")
	}
}

func TestShortHash_ClampsOverlongN(t *testing.T) {
	// sha256 hex is 64 chars; asking for more must not panic and must clamp.
	h := ShortHash("x", 999)
	if len(h) != 64 {
		t.Errorf("shortHash(x, 999) len = %d, want 64 (clamped)", len(h))
	}
}

func TestCache_PutGetRoundTrip(t *testing.T) {
	c := NewMemCache(true)
	want := &Rendered{JS: "console.log(1)", JSName: "a.js"}
	c.Put(NewMemCacheKey("comp", `{"a":1}`), want)

	got, ok := c.Get(NewMemCacheKey("comp", `{"a":1}`))
	if !ok {
		t.Fatal("get after put returned ok=false")
	}
	if got != want {
		t.Errorf("get returned %+v, want same pointer %+v", got, want)
	}
}

// Clear resets the cache. Leaving byName populated leaks a key set per
// component for the lifetime of the process and leaves the reverse index
// describing entries that are gone.
func TestCache_ClearEmptiesBothIndexes(t *testing.T) {
	c := NewMemCache(true)
	c.Put(NewMemCacheKey("comp", "root"), &Rendered{JS: "x"})
	c.Clear()

	if _, ok := c.Get(NewMemCacheKey("comp", "root")); ok {
		t.Error("Clear left an entry behind")
	}
	c.mu.RLock()
	stale := len(c.byName)
	c.mu.RUnlock()
	if stale != 0 {
		t.Errorf("Clear left %d stale byName buckets", stale)
	}
}

// Two artifacts built under different settings are different artifacts. Nothing
// else in the key distinguishes them, so the build fingerprint has to.
func TestCache_BuildFingerprintSeparatesEntries(t *testing.T) {
	c := NewMemCache(true)
	c.Put(NewBuildCacheKey("comp", "root", "minified"), &Rendered{JS: "min"})
	c.Put(NewBuildCacheKey("comp", "root", "readable"), &Rendered{JS: "raw"})

	got, ok := c.Get(NewBuildCacheKey("comp", "root", "minified"))
	if !ok || got.JS != "min" {
		t.Fatalf("build fingerprint did not key the entry: ok=%v got=%+v", ok, got)
	}
	// Invalidation is by component, so it must still reach both.
	c.InvalidateComponent("comp")
	if _, ok := c.Get(NewBuildCacheKey("comp", "root", "readable")); ok {
		t.Error("InvalidateComponent missed an entry under a different build fingerprint")
	}
}

func TestCache_MissReturnsFalse(t *testing.T) {
	c := NewMemCache(true)
	if _, ok := c.Get(NewMemCacheKey("absent", "")); ok {
		t.Error("get on empty cache returned ok=true")
	}
}

func TestCache_DisabledNeverStores(t *testing.T) {
	c := NewMemCache(false)
	c.Put(NewMemCacheKey("k", ""), &Rendered{JS: "x"})
	if _, ok := c.Get(NewMemCacheKey("k", "")); ok {
		t.Error("disabled cache returned a stored value")
	}
}

func TestCache_ConcurrentAccessIsSafe(t *testing.T) {
	// Run with -race to make this meaningful.
	c := NewMemCache(true)
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n%4) // deliberate contention on few keys
			for j := 0; j < 200; j++ {
				c.Put(NewMemCacheKey(key, ""), &Rendered{JS: fmt.Sprintf("%d-%d", n, j)})
				c.Get(NewMemCacheKey(key, ""))
			}
		}(i)
	}
	wg.Wait()
}
