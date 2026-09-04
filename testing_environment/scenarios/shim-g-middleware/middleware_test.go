// Package shim_g is a scenario about registering more than one middleware.
//
// It is a report reduced to a test: a consumer registered two, one putting the
// signed-in user on the page as a data island and one putting the service's
// version there. The second appeared. The first never did, and never logged a
// line from inside itself either — the factory that builds it ran, so the
// registration was reached, and the middleware it returned was not.
//
// RequestBehaviourBuilder.With assigned the slice it was given instead of
// appending to it, so each call discarded every middleware registered before
// it — the bundler's defaults included. One middleware always worked, and the
// second one silently replaced the first.
//
// # Its own module
//
// gorilla/mux and gorilla/sessions are dependencies of this scenario and not of
// go-solid. The scenario carries a go.mod and the library's graph never sees
// them.
package shim_g

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

// boot stages the fixture into a temp tree and runs the consumer's
// InitAppState against it, so a boot may write its workspace without dirtying
// the checkout. The chdir is what lets extract.go stay verbatim: it resolves
// its components and assets from the working directory.
func boot(t *testing.T) *AppState {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{"frontend", "static"} {
		if err := os.CopyFS(filepath.Join(root, dir), os.DirFS(dir)); err != nil {
			t.Fatalf("stage %s: %v", dir, err)
		}
	}
	t.Chdir(root)

	state, err := InitAppState(&AppConfig{Mode: ModeDevelopment}, mux.NewRouter())
	if err != nil {
		t.Fatalf("InitAppState: %v", err)
	}
	t.Cleanup(state.Templating.Close)
	return state
}

// login writes the session the user island reads and returns a request
// carrying the cookie it was written into.
//
// The cookie is the whole of the transport: the middleware is handed the
// request, not the session, and reads it back out of the store itself. A
// request that does not carry it is an anonymous one, whatever was saved.
func login(t *testing.T, state *AppState) *http.Request {
	t.Helper()

	writer := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	session, err := state.CookieStore.Get(request, state.ServiceSessionName)
	if err != nil {
		t.Fatalf("opening the session: %v", err)
	}

	user, err := state.UserService.Login("doesnt", "matter")
	if err != nil {
		t.Fatalf("logging in: %v", err)
	}

	// The keys the session is written under have to be the keys the middleware
	// reads: it looks the username up under the name InitAppState chose, not
	// under one the caller picks here.
	UpdateUserSession(&ApiContext{
		UserSessionName:     state.SessionName,
		WorkspaceSessionKey: "workspace",
		ServiceSessionName:  state.ServiceSessionName,
	}, session, user)

	if err := session.Save(request, writer); err != nil {
		t.Fatalf("saving the session: %v", err)
	}

	next := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, cookie := range writer.Result().Cookies() {
		next.AddCookie(cookie)
	}
	return next
}

type ApiContext struct {
	UserSessionName     string
	WorkspaceSessionKey string
	ServiceSessionName  string
}

func UpdateUserSession(context *ApiContext, session *sessions.Session, user *UserDataDTO) {
	session.Values[context.UserSessionName] = user.Username
	session.Values[context.WorkspaceSessionKey] = user.Workspace
	session.Options = &sessions.Options{
		MaxAge:   3600, // Set max age 60 mins
		HttpOnly: true,
		Secure:   true, // Set to true if using HTTPS
		SameSite: http.SameSiteLaxMode,
		Path:     "/", // Set the path for which the cookie is valid
	}
}

func render(t *testing.T, state *AppState, request *http.Request) string {
	t.Helper()

	res, err := state.Templating.
		Anonymous(`() => <div id="shim-g">.</div>`, nil).
		ForRequest(httptest.NewRecorder(), request).
		Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return res.HTML
}

// The report
// ---------------------------------------------------------------------------
// Two middleware registered, one served. The one that survived was the one
// registered last, which is the shape of an assignment rather than an append.

func Test_bothDataIslandsAreIncluded(t *testing.T) {
	state := boot(t)
	html := render(t, state, login(t, state))

	for _, key := range []string{KEY_OF_LOCAL_USER_DATA, KEY_OF_HOTS_SERVICE_STATISTICS} {
		if !strings.Contains(html, key) {
			t.Errorf("data island %q is absent; its middleware was registered and never ran:\n%s", key, html)
		}
	}
	// The island being present is not the same as it carrying the user: an
	// empty one would pass the check above and tell the browser nothing.
	if !strings.Contains(html, "shim g testing") {
		t.Errorf("%q is present but does not carry the signed-in user:\n%s", KEY_OF_LOCAL_USER_DATA, html)
	}
}

// The user island declines on a request with no session, and declining is not
// failing: the page still renders, and the middleware registered after it
// still runs.
func Test_anAnonymousRequestStillGetsTheStatistics(t *testing.T) {
	state := boot(t)
	html := render(t, state, httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Contains(html, KEY_OF_LOCAL_USER_DATA) {
		t.Errorf("%q was written for a request carrying no session:\n%s", KEY_OF_LOCAL_USER_DATA, html)
	}
	if !strings.Contains(html, KEY_OF_HOTS_SERVICE_STATISTICS) {
		t.Errorf("%q is absent; a middleware that declined stopped the ones after it:\n%s", KEY_OF_HOTS_SERVICE_STATISTICS, html)
	}
}

// A data island belongs to the render that produced it. The artifact behind it
// is a cache entry every request for that component shares, so an island
// written onto the artifact is one user's session on the next user's page.
func Test_theUsersIslandDoesNotOutliveTheirRequest(t *testing.T) {
	state := boot(t)

	if html := render(t, state, login(t, state)); !strings.Contains(html, "shim g testing") {
		t.Fatalf("the first render did not carry the user, so the check below would pass vacuously:\n%s", html)
	}

	// Same component, same props: a cache hit, and an anonymous caller.
	html := render(t, state, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(html, "shim g testing") {
		t.Errorf("the previous request's user was served to a caller that never signed in:\n%s", html)
	}
}
