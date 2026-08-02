package caching

import (
	"fmt"
	"sync"
	"testing"
)

func Test_Deterministic(t *testing.T) {
	a := NewMemCacheKey("auth/LoginForm", `{"title":"Hi"}`)
	b := NewMemCacheKey("auth/LoginForm", `{"title":"Hi"}`)
	if a.String() != b.String() {
		t.Errorf("caching.MemCacheKey not deterministic: %q != %q", a, b)
	}
}

func Test_Key_SensitiveToEachInput(t *testing.T) {
	base := NewMemCacheKey("comp", `{"a":1}`)

	cases := map[string]*MemCacheKey{
		"different name":  NewMemCacheKey("other", `{"a":1}`),
		"different props": NewMemCacheKey("comp", `{"a":2}`),
	}
	for label, got := range cases {
		if got == base {
			t.Errorf("caching.MemCacheKey collision for %s: key did not change", label)
		}
	}
}

// The separator bytes matter: without them, name="ab"+props="c" would collide
// with name="a"+props="bc". Guard against that regression.
func Test_cacheKey_NoConcatenationCollision(t *testing.T) {
	x := NewMemCacheKey("ab", "c")
	y := NewMemCacheKey("a", "bc")
	if x == y {
		t.Error("caching.MemCacheKey collides across the name/props boundary (missing separator)")
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
