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
	assertAccepted(t, "the original contract", typeFault(t, b, "pages/Dashboard", props))

	rewrite(t, p.componentFile("pages/Dashboard.tsx"), `
export default function Dashboard(props: {
  title: string;
  unread: number;
  accountId: string;
}) {
  return <section data-account={props.accountId}>{props.title}</section>;
}
`)

	assertRejected(t, "the newly required prop",
		typeFault(t, b, "pages/Dashboard", props), "pages/Dashboard", "accountId")
}

// The definition is regenerated — the component itself never changes. Nothing
// but the recorded source digests can catch this, and HMR cannot help: a
// type-only import is erased before the bundler ever sees it.
func TestRegeneratingAnImportedDefinitionInvalidatesItsDependents(t *testing.T) {
	p := newProject(t)
	b := p.boot(t, shared_types.CHECK_RUNTIME)

	props := articleProps{Slug: "hello-world", CurrentPath: "/blog/hello-world"}
	assertAccepted(t, "the original composed contract", typeFault(t, b, "pages/Article", props))

	rewrite(t, filepath.Join(p.workspace, shared_types.TYPES_DIR_NAME, "navigation.d.ts"), `
export interface Navigation {
  currentPath: string;
  backHref?: string;
  locale: string;
}
`)

	assertRejected(t, "a requirement added to an imported definition",
		typeFault(t, b, "pages/Article", props), "pages/Article", "locale")
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
	assertAccepted(t, "a restart over an unchanged tree",
		typeFault(t, second, "pages/Dashboard", dashboardProps{Title: "Inbox", Unread: 1}))

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
	assertRejected(t, "the contract the edited component now states",
		typeFault(t, second, "pages/Profile", profileProps{UserID: "u-1", DisplayName: "Lily"}),
		"pages/Profile", "tenant")
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
