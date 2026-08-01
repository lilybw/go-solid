package hmr

import (
	"net/http"
	"testing"

	"github.com/lilybw/go-solid/shared"
)

func TestNormalizeHMRConfig_NilConfigWithoutMuxErrors(t *testing.T) {
	// A nil config means "no mux supplied", which must error under the
	// mux-required rule (defaulting to DefaultServeMux would silently break HMR).
	if _, err := NormalizeHMRConfig(nil); err == nil {
		t.Fatal("expected error for nil config (no mux), got nil")
	}
}

func TestNormalizeHMRConfig_MissingMuxErrors(t *testing.T) {
	_, err := NormalizeHMRConfig(&shared.HMRConfig{HMRPath: "/x"})
	if err == nil {
		t.Fatal("expected error when Mux is nil, got nil")
	}
}

func TestNormalizeHMRConfig_DefaultsPathWhenEmpty(t *testing.T) {
	mux := http.NewServeMux()
	got, err := NormalizeHMRConfig(&shared.HMRConfig{Mux: mux})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HMRPath != DEFAULT_HMR_PATH {
		t.Fatalf("expected default path %q, got %q", DEFAULT_HMR_PATH, got.HMRPath)
	}
}

func TestNormalizeHMRConfig_KeepsExplicitPath(t *testing.T) {
	mux := http.NewServeMux()
	got, err := NormalizeHMRConfig(&shared.HMRConfig{Mux: mux, HMRPath: "/custom_hmr"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.HMRPath != "/custom_hmr" {
		t.Fatalf("expected /custom_hmr, got %q", got.HMRPath)
	}
}

func TestNormalizeHMRConfig_PreservesMux(t *testing.T) {
	mux := http.NewServeMux()
	got, err := NormalizeHMRConfig(&shared.HMRConfig{Mux: mux})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Mux != mux {
		t.Fatal("normalize must preserve the caller's mux, not replace it")
	}
}

// TestHub_ReloadOnEmptyIsNoop ensures reloading a component with no connections
// does not panic and does not block. This exercises the snapshot-under-lock path
// with a zero-length target set.
func TestHub_ReloadOnEmptyIsNoop(t *testing.T) {
	h := NewHub(&shared.HMRConfig{})
	done := make(chan struct{})
	go func() {
		h.Reload("ui/NoOne")
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfter():
		t.Fatal("Reload on empty hub blocked or hung")
	}
}
