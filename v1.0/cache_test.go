package go_solid

import (
	"fmt"
	"sync"
	"testing"
)

func TestCacheKey_Deterministic(t *testing.T) {
	a := cacheKey("auth/LoginForm", `{"title":"Hi"}`, true)
	b := cacheKey("auth/LoginForm", `{"title":"Hi"}`, true)
	if a != b {
		t.Errorf("cacheKey not deterministic: %q != %q", a, b)
	}
}

func TestCacheKey_SensitiveToEachInput(t *testing.T) {
	base := cacheKey("comp", `{"a":1}`, true)

	cases := map[string]string{
		"different name":   cacheKey("other", `{"a":1}`, true),
		"different props":  cacheKey("comp", `{"a":2}`, true),
		"different minify": cacheKey("comp", `{"a":1}`, false),
	}
	for label, got := range cases {
		if got == base {
			t.Errorf("cacheKey collision for %s: key did not change", label)
		}
	}
}

// The separator bytes matter: without them, name="ab"+props="c" would collide
// with name="a"+props="bc". Guard against that regression.
func TestCacheKey_NoConcatenationCollision(t *testing.T) {
	x := cacheKey("ab", "c", false)
	y := cacheKey("a", "bc", false)
	if x == y {
		t.Error("cacheKey collides across the name/props boundary (missing separator)")
	}
}

func TestShortHash_LengthAndStability(t *testing.T) {
	h1 := shortHash("hello world", 8)
	h2 := shortHash("hello world", 8)
	if h1 != h2 {
		t.Errorf("shortHash not stable: %q != %q", h1, h2)
	}
	if len(h1) != 8 {
		t.Errorf("shortHash len = %d, want 8", len(h1))
	}
}

func TestShortHash_DifferentInputsDiffer(t *testing.T) {
	if shortHash("a", 8) == shortHash("b", 8) {
		t.Error("shortHash produced same prefix for different inputs")
	}
}

func TestShortHash_ClampsOverlongN(t *testing.T) {
	// sha256 hex is 64 chars; asking for more must not panic and must clamp.
	h := shortHash("x", 999)
	if len(h) != 64 {
		t.Errorf("shortHash(x, 999) len = %d, want 64 (clamped)", len(h))
	}
}

func TestCache_PutGetRoundTrip(t *testing.T) {
	c := newCache(true)
	want := &Rendered{JS: "console.log(1)", JSName: "a.js"}
	c.put("k", want)

	got, ok := c.get("k")
	if !ok {
		t.Fatal("get after put returned ok=false")
	}
	if got != want {
		t.Errorf("get returned %+v, want same pointer %+v", got, want)
	}
}

func TestCache_MissReturnsFalse(t *testing.T) {
	c := newCache(true)
	if _, ok := c.get("absent"); ok {
		t.Error("get on empty cache returned ok=true")
	}
}

func TestCache_DisabledNeverStores(t *testing.T) {
	c := newCache(false)
	c.put("k", &Rendered{JS: "x"})
	if _, ok := c.get("k"); ok {
		t.Error("disabled cache returned a stored value")
	}
}

func TestCache_ConcurrentAccessIsSafe(t *testing.T) {
	// Run with -race to make this meaningful.
	c := newCache(true)
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", n%4) // deliberate contention on few keys
			for j := 0; j < 200; j++ {
				c.put(key, &Rendered{JS: fmt.Sprintf("%d-%d", n, j)})
				c.get(key)
			}
		}(i)
	}
	wg.Wait()
}
