//go:build linux

package clipboard

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// All of these run against SECONDARY rather than CLIPBOARD on purpose: they
// claim and drop a selection for real, and doing that to CLIPBOARD would wipe
// whatever the person running the tests had copied.

// TestPublishThenRelease pins the whole contract of the insert route: the text
// is readable while the release func is unspent, and the selection is gone
// once it is called. "Gone" rather than "restored" is the deal — see the
// package comment.
func TestPublishThenRelease(t *testing.T) {
	requireXclip(t)

	release, err := publishTo("secondary", "published-text")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	if got := settled(t, "secondary", "published-text"); got != "published-text" {
		t.Fatalf("while published: got %q, want published-text", got)
	}

	release()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := readTextFrom("secondary"); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the selection still had an owner after release")
}

// TestReleaseIsIdempotent matters because the release func is both deferred by
// the insert route and, on the Set path, called again when the next Set
// supersedes it. A second call must not kill an unrelated process that has
// since inherited the pid.
func TestReleaseIsIdempotent(t *testing.T) {
	requireXclip(t)

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
	requireXclip(t)

	release, err := publishTo("secondary", "")
	if err != nil {
		t.Fatalf("publishTo: %v", err)
	}
	defer release()

	if s, ok := readTextFrom("secondary"); ok {
		t.Fatalf("empty selection reported as content: %q", s.Text)
	}
}

func requireXclip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip not available")
	}
}

// settled polls until the selection serves want. Claiming a selection is
// asynchronous — Publish detaches xclip via Start, so the new owner may not be
// ready the instant we read.
func settled(t *testing.T, selection, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if s, ok := readTextFrom(selection); ok {
			last = strings.TrimSpace(s.Text)
			if last == want {
				return last
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return last
}
