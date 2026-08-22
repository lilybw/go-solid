package shim_b

// The parts of the workflow that happen over time: a developer editing a
// component, a definition being regenerated underneath them, a restart, and a
// component being deleted.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	shared_types "github.com/lilybw/go-solid/shared/types"
)

// rewrite replaces a file and moves its timestamp on, so the change is
// unmistakable whatever the filesystem's granularity.
func rewrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("rewrite %q: %v", path, err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("touch %q: %v", path, err)
	}
}

// A developer adds a required prop to a component. The next request has to be
// held against the new contract, with no restart and no explicit invalidation.
func TestEditingAComponentChangesWhatPropsMustSatisfy(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	props := dashboardProps{Title: "Inbox", Unread: 3}
	assertQuiet(t, "the original contract",
		observe(t, func() { b.Prepare("pages/Dashboard", props) }))

	rewrite(t, p.componentFile("pages/Dashboard.tsx"), `
export default function Dashboard(props: {
  title: string;
  unread: number;
  accountId: string;
}) {
  return <section data-account={props.accountId}>{props.title}</section>;
}
`)

	out := observe(t, func() { b.Prepare("pages/Dashboard", props) })

	assertReports(t, "the newly required prop", out, "pages/Dashboard", "accountId")
}

// The definition is regenerated — the component itself never changes. Nothing
// but the recorded source digests can catch this, and HMR cannot help: a
// type-only import is erased before the bundler ever sees it.
func TestRegeneratingAnImportedDefinitionInvalidatesItsDependents(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	props := articleProps{Slug: "hello-world", CurrentPath: "/blog/hello-world"}
	assertQuiet(t, "the original composed contract",
		observe(t, func() { b.Prepare("pages/Article", props) }))

	rewrite(t, filepath.Join(p.workspace, shared_types.TYPES_DIR_NAME, "navigation.d.ts"), `
export interface Navigation {
  currentPath: string;
  backHref?: string;
  locale: string;
}
`)

	out := observe(t, func() { b.Prepare("pages/Article", props) })

	assertReports(t, "a requirement added to an imported definition", out, "pages/Article", "locale")
}

// Restarting reuses what the previous run worked out, rather than rewriting it.
func TestRestartReusesTheCacheOnDisk(t *testing.T) {
	p := newProject(t)
	first := p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)
	first.Close()

	entry := p.cacheEntry("pages/Dashboard")
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(entry, past, past); err != nil {
		t.Fatal(err)
	}

	second := p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)
	out := observe(t, func() {
		second.Prepare("pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 1})
	})
	assertQuiet(t, "a restart over an unchanged tree", out)

	info, err := os.Stat(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Error("an unchanged entry should be reused, not rewritten")
	}
}

// A stale entry from a previous run must not be trusted after the component it
// describes has changed underneath it.
func TestRestartRejectsAnEntryWhoseComponentChanged(t *testing.T) {
	p := newProject(t)
	first := p.boot(t, shared_types.CHECK_RUNTIME)
	first.Close()

	rewrite(t, p.componentFile("pages/Profile.tsx"), `
interface ProfileProps {
  userId: string;
  displayName: string;
  tenant: string;
}

export default function Profile(props: ProfileProps) {
  return <h1>{props.displayName}</h1>;
}
`)

	second := p.boot(t, shared_types.CHECK_RUNTIME)
	out := observe(t, func() {
		second.Prepare("pages/Profile", profileProps{UserID: "u-1", DisplayName: "Lily"})
	})

	assertReports(t, "the contract the edited component now states", out, "pages/Profile", "tenant")
}

// Deleting a component leaves an entry behind; the next boot clears it.
func TestDeletedComponentIsPrunedFromTheCache(t *testing.T) {
	p := newProject(t)
	first := p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)
	first.Close()

	entry := p.cacheEntry("widgets/Clock")
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("expected an entry to prune: %v", err)
	}
	if err := os.Remove(p.componentFile("widgets/Clock.tsx")); err != nil {
		t.Fatal(err)
	}

	p.boot(t, shared_types.CHECK_RUNTIME_AND_BOOT)

	if _, err := os.Stat(entry); err == nil {
		t.Error("the entry for a deleted component should have been pruned")
	}
	if _, err := os.Stat(p.cacheEntry("pages/Dashboard")); err != nil {
		t.Errorf("a live entry must survive pruning: %v", err)
	}
}
