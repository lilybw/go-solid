package shim_f

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	go_solid "github.com/lilybw/go-solid"
	"github.com/lilybw/go-solid/shared/compat"
	shared_esbuild "github.com/lilybw/go-solid/shared/esbuild"
	shared_hmr "github.com/lilybw/go-solid/shared/hmr"
	"github.com/lilybw/go-solid/shared/logging"
	shared_static "github.com/lilybw/go-solid/shared/static"
	shared_types "github.com/lilybw/go-solid/shared/types"
)

// The asset the consumer could not fetch.
const LOGO = "img/hots_logo_p.svg"

type project struct {
	components string
	assets     string
}

// staged copies the fixture into a temp tree, so a boot may write its workspace
// without dirtying the checkout.
func staged(t *testing.T) *project {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{"components", "static"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(dir)); err != nil {
			t.Fatalf("stage %s: %v", dir, err)
		}
	}
	return &project{
		components: filepath.Join(root, "components"),
		assets:     filepath.Join(root, "static"),
	}
}

// boot is the consumer's InitAppState reduced to what the mount depends on.
// Bundling and type checking are off: this scenario is about routing, and it
// should run without a toolchain.
func boot(t *testing.T, p *project, static, hmr compat.MuxLike) (*go_solid.Bundler, error) {
	t.Helper()

	cfg := &go_solid.Config{
		Components: p.components,
		LogLevel:   logging.LEVEL_ERROR,
		Generation: &shared_esbuild.BundlerConfig{Disabled: true},
		Types:      &shared_types.TypesConfig{Check: shared_types.CHECK_NEVER},
		Static:     &shared_static.StaticConfig{Location: p.assets, Mux: static},
	}
	if hmr != nil {
		cfg.HMR = &shared_hmr.HMRConfig{Mux: hmr}
	}

	bundler, err := go_solid.New(cfg)
	if bundler != nil {
		t.Cleanup(bundler.Close)
	}
	return bundler, err
}

func get(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The report
// ---------------------------------------------------------------------------
// A gorilla/mux router, a static directory mirrored correctly, a URL carrying
// the right prefix and content hash — and 404 for every asset, by hand as well
// as from the page. gorilla's Handle registers Path, which matches that one
// path and nothing below it, so the endpoint answered for the mount and for
// nothing it mounts.

func TestGorillaMuxServesAssetsBelowTheMount(t *testing.T) {
	router := mux.NewRouter()
	bundler, err := boot(t, staged(t), compat.MuxLikeFromRouterLike[*mux.Route](router), nil)
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}

	url := bundler.Static().URL(LOGO)
	if url == "" {
		t.Fatalf("%s was not published", LOGO)
	}
	if !strings.HasPrefix(url, shared_static.DEFAULT_MOUNT_PATH) {
		t.Fatalf("%s is not under the mount", url)
	}

	rec := get(t, router, url)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s returned %d; the endpoint is registered but not as a subtree", url, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want \"image/svg+xml\"", got)
	}
}

// The socket is registered without a trailing slash and has to stay an exact
// match, or the fix for the endpoint would hand it everything beginning with
// its path.
func TestTheHotReloadSocketStaysExact(t *testing.T) {
	router := mux.NewRouter()
	if _, err := boot(t, staged(t),
		compat.MuxLikeFromRouterLike[*mux.Route](router),
		compat.MuxLikeFromRouterLike[*mux.Route](router),
	); err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}

	socket := shared_hmr.NIL_HMR_CONFIG.Path
	if rec := get(t, router, socket); rec.Code == http.StatusNotFound {
		t.Errorf("%s returned 404; the socket is not mounted", socket)
	}
	if rec := get(t, router, socket+"/nested"); rec.Code != http.StatusNotFound {
		t.Errorf("%s returned %d, want 404; the socket was registered as a subtree", socket, rec.Code)
	}
}

// exactly is the registration the consumer had before detection: straight
// through to gorilla's Handle, which is a path match. Kept as a stand-in for
// every router whose subtree mechanism go_solid does not recognise.
type exactly struct{ router *mux.Router }

func (this *exactly) Handle(pattern string, handler http.Handler) {
	this.router.Handle(pattern, handler)
}

func (this *exactly) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	this.router.ServeHTTP(w, r)
}

// An endpoint that answers 404 for everything it serves is not a working boot,
// and it is the thing the report describes. Detection is what stops it here;
// the probe is what stops it on a router detection does not know.
func TestAnEndpointThatCannotServeIsRefusedAtBoot(t *testing.T) {
	router := mux.NewRouter()
	_, err := boot(t, staged(t), &exactly{router: router}, nil)
	if err == nil {
		t.Fatal("boot succeeded with an endpoint that 404s every asset")
	}
	for _, want := range []string{shared_static.DEFAULT_MOUNT_PATH, compat.STRATEGY_HANDLE} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q, so it does not say what went wrong:\n%v", want, err)
		}
	}
}

// The escape hatch takes the consumer's word for it: nothing is detected behind
// a registration function, and a function is not a router, so there is nothing
// to ask afterwards either. Asserted rather than assumed, because it is the one
// path on which the two guards above are both absent.
func TestTheEscapeHatchIsUnchecked(t *testing.T) {
	router := mux.NewRouter()
	bundler, err := boot(t, staged(t), compat.MuxLikeFromFunc(
		func(pattern string, handler http.Handler) { router.Handle(pattern, handler) },
	), nil)
	if err != nil {
		t.Fatalf("go_solid.New: %v", err)
	}
	if rec := get(t, router, bundler.Static().URL(LOGO)); rec.Code != http.StatusNotFound {
		t.Errorf("returned %d; the registration is wrong and only the consumer can know it", rec.Code)
	}
}
