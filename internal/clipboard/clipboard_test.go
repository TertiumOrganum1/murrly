//go:build linux

package clipboard

import (
	"os"
	"testing"
)

// All of these run against SECONDARY rather than CLIPBOARD on purpose: they
// claim and drop a selection for real, and doing that to CLIPBOARD would wipe
// whatever the person running the tests had copied.

func requireX(t *testing.T) *xOwner {
	t.Helper()
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	o, err := selectionOwner()
	if err != nil {
		t.Skipf("no selection owner: %v", err)
	}
	return o
}

// readBack is the snapshot path pointed at one selection, which is also the
// shape of a paste: ask what is on offer, then take the best text flavour.
func readBack(t *testing.T, o *xOwner, selection string) (Saved, bool) {
	t.Helper()
	return readTextFrom(o, selection, readTargets(o, selection))
}

// TestPublishThenRelease pins the whole contract of the insert route: the text
// is readable while the release func is unspent, and the selection is gone
// once it is called. "Gone" rather than "restored" is the deal — see the
// package comment.
func TestPublishThenRelease(t *testing.T) {
	o := requireX(t)

	release, err := publishTo("secondary", "published-text")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	got, ok := readBack(t, o, "secondary")
	if !ok || got.Text != "published-text" {
		t.Fatalf("while published: got %q (ok=%v), want published-text", got.Text, ok)
	}

	release()
	if s, ok := readBack(t, o, "secondary"); ok {
		t.Fatalf("the selection still had content after release: %q", s.Text)
	}
}

// TestReleaseIsIdempotent matters because the release func is both deferred by
// the insert route and, on the Set path, called again when the next publish
// supersedes it.
func TestReleaseIsIdempotent(t *testing.T) {
	requireX(t)

	release, err := publishTo("secondary", "twice-released")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	release()
	release()
}

// TestReadTextIgnoresAnEmptySelection pins the guard behind Stash.Put: an
// empty read is not content, and treating it as a snapshot would let the menu
// offer to "restore" a wipe.
func TestReadTextIgnoresAnEmptySelection(t *testing.T) {
	o := requireX(t)

	release, err := publishTo("secondary", "")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	if s, ok := readBack(t, o, "secondary"); ok {
		t.Fatalf("empty selection reported as content: %q", s.Text)
	}
}

// A dictation is published and read back through the X server whole, including
// the multi-byte characters a wrong property format would cut in half.
func TestPublishedTextSurvivesTheRoundTrip(t *testing.T) {
	o := requireX(t)

	const text = "Проверка круговорота — «кавычки», тире и ещё ёжик."
	release, err := publishTo("secondary", text)
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	got, ok := readBack(t, o, "secondary")
	if !ok || got.Text != text {
		t.Fatalf("got %q (ok=%v), want %q", got.Text, ok, text)
	}
}

// The package reads nobody's clipboard until EnableSnapshot says so. The
// config turns it on (and defaults to on), but a caller that never wires the
// config gets no reads of somebody else's selection — on X11 the snapshot is
// the one operation here that waits on another application.
func TestSnapshotIsInertUntilEnabled(t *testing.T) {
	requireX(t)

	release, err := publishTo("secondary", "content")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	if snapshotting.Load() {
		t.Fatal("snapshotting is on before anything enabled it")
	}
	if _, ok := New().readSystemSnapshot(); ok {
		t.Fatal("readSystemSnapshot returned content with snapshotting off")
	}
}
