//go:build linux

package clipboard

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The X11 clipboard is not a store — it is a process. Whoever "copied" keeps
// the bytes in its own memory and hands them over on request, so text exists
// in the clipboard only for as long as some process owns the selection.
//
// That is the whole design constraint here. Murrly used to own CLIPBOARD from
// the first dictation until it exited, which meant every Ctrl+V anywhere on
// the desktop was served by a child process of ours — and any hiccup of ours
// became a hiccup of the whole desktop's clipboard.
//
// So ownership is now scoped to the paste that needs it. Publish hands back a
// release func; the insert route presses the chord, waits out a short hold and
// releases. Everything else the old implementation did — counting fetches via
// `xclip -verbose`, polling to confirm ownership, snapshotting foreign binary
// targets and republishing them, waiting for a read that Chromium never
// performs — is gone. The user's previous clipboard is kept as plain text in
// our own memory (see ReadText and Stash) instead of being round-tripped
// through X, which is what used to degrade it to a single target.

// readTimeout caps the one read we make of somebody else's selection. The read
// leaves the process and is answered by whatever owns the clipboard, so a hung
// owner must cost the dictation nothing: the snapshot is a courtesy, and the
// recording it runs alongside must not wait for it.
const readTimeout = 400 * time.Millisecond

// maxStashedImage caps what a snapshot will hold in RSS. A screenshot of a
// region is a few hundred KB and a full 4K screen a few MB; past this we are
// storing somebody's scanned document for the rest of the session, and the
// point of the stash is undoing an accident, not archiving.
const maxStashedImage = 32 << 20

// imageTargets are the picture formats we will keep, best first. PNG is what
// every screenshot tool on this desktop offers and it is lossless; the others
// are there so a snapshot from an odd source is not dropped on the floor.
var imageTargets = []string{"image/png", "image/webp", "image/jpeg", "image/bmp", "image/tiff"}

// readSystemSnapshot is the X11 half of Snapshot (see clipboard.go, which
// filters out our own dictations before the caller sees them).
func (c *Clipboard) readSystemSnapshot() (Saved, bool) { return readSnapshotFrom("clipboard") }

// readSnapshotFrom is readSystemSnapshot against a named selection. Split out
// so the tests can exercise the real xclip round-trip on SECONDARY instead of
// trampling the clipboard of whoever is running them.
//
// TARGETS comes first and is not an optimisation — it is the only way to know
// what is in there. xclip hands back its payload for ANY target asked of it,
// regardless of what the owner advertises: ask an image-only selection for
// UTF8_STRING and you get PNG bytes back, rc=0, looking exactly like text.
// Reading blind is how binary ends up stashed as a string. TARGETS costs 3-7 ms
// against a live owner, which is nothing on a path nobody waits for.
func readSnapshotFrom(selection string) (Saved, bool) {
	targets := readTargets(selection)
	if len(targets) == 0 {
		return Saved{}, false
	}
	// Text wins when both are on offer: it is the thing that can actually go
	// back into the clipboard, and a picture is only ever a file on disk.
	for _, want := range textTargets {
		if hasTarget(targets, want) {
			return readTextFrom(selection)
		}
	}
	for _, want := range imageTargets {
		if !hasTarget(targets, want) {
			continue
		}
		out, ok := fetchTarget(selection, want)
		if !ok || len(out) == 0 {
			return Saved{}, false
		}
		if len(out) > maxStashedImage {
			log.Printf("clipboard: the picture in the clipboard is %d MB; not keeping it", len(out)>>20)
			return Saved{}, false
		}
		return Saved{HasContent: true, Image: out, ImageTarget: want}, true
	}
	return Saved{}, false
}

// readTargets asks the selection owner what formats it can produce. An owner
// that does not answer in time leaves us with nothing, which is the correct
// outcome: no snapshot beats a wrong one.
func readTargets(selection string) []string {
	out, ok := fetchTarget(selection, "TARGETS")
	if !ok {
		return nil
	}
	var targets []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			targets = append(targets, line)
		}
	}
	return targets
}

func hasTarget(targets []string, want string) bool {
	for _, t := range targets {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

// fetchTarget runs one conversion request under the read timeout.
func fetchTarget(selection, target string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xclip", "-selection", selection, "-o", "-t", target).Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("clipboard: the current owner did not answer %s within %v; not keeping a snapshot", target, readTimeout)
		}
		return nil, false
	}
	return out, true
}

// textTargets are the plain-text flavours a selection owner may offer, best
// first. Owners differ on which they advertise — GTK leads with UTF8_STRING,
// older Qt with STRING, browsers with text/plain;charset=utf-8.
var textTargets = []string{"UTF8_STRING", "text/plain;charset=utf-8", "text/plain", "STRING"}

// readTextFrom reads the plain text of a selection. In practice this is one
// fork: an owner that has text answers the first flavour asked. The loop is
// for the owner that advertises only one of the older spellings.
func readTextFrom(selection string) (Saved, bool) {
	for _, target := range textTargets {
		if out, ok := fetchTarget(selection, target); ok && len(out) > 0 {
			return Saved{Text: string(out), HasContent: true}, true
		}
	}
	return Saved{}, false
}

// Publish makes text the content of the clipboard and returns the func that
// gives the clipboard back. The caller owns the selection for exactly as long
// as it holds onto that func, which for the insert route is the paste chord
// plus a short hold.
//
// -verbose is load-bearing, and not for its output: run silently, xclip forks
// itself into the background and the parent exits immediately, so the process
// we started is not the process that owns the selection and killing it
// releases nothing. In verbose mode it stays in the foreground and stays
// killable. Its chatter goes to /dev/null (a nil Stderr), so nothing has to be
// drained — draining that pipe is what used to tie the liveness of the whole
// desktop's clipboard to whether Murrly was keeping up.
//
// No ownership confirmation either: xclip claims within a couple of ms, and
// polling for it cost a fork/exec every 20 ms.
func (c *Clipboard) Publish(text string) (func(), error) {
	release, err := publishTo("clipboard", text)
	if err == nil {
		markOurs(text)
	}
	return release, err
}

// publishTo is Publish against a named selection — see readTextFrom.
func publishTo(selection, text string) (func(), error) {
	cmd := exec.Command("xclip", "-selection", selection, "-verbose", "-i")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return func() {}, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			// Killing an already-exited process just errors, which is fine —
			// xclip exits on its own the moment another client claims the
			// selection.
			_ = cmd.Process.Kill()
			go func() { _ = cmd.Wait() }()
		})
	}, nil
}

// Set puts text in the clipboard and leaves it there — for the tray items and
// the paste-last binding, where the user explicitly asked for something to be
// in the clipboard. Ownership is kept (it has to be: drop it and the text is
// gone) but tracked, so Release can hand it back rather than orphan an xclip
// that outlives us.
func (c *Clipboard) Set(text string) error {
	release, err := publishTo("clipboard", text)
	if err != nil {
		return err
	}
	// The user asked for this text to be in the clipboard, so it counts as
	// theirs from now on — the next dictation may snapshot it.
	forgetOurs()
	ownersMu.Lock()
	prev := owner
	owner = release
	ownersMu.Unlock()
	// The previous holder loses the selection to the new one anyway; releasing
	// it explicitly just reaps the process now instead of at exit.
	if prev != nil {
		prev()
	}
	return nil
}

// owner is the release func for the selection Set is currently holding, if
// any. Only the latest matters: claiming the selection makes the X server take
// it off the previous owner, which then exits on its own.
var (
	ownersMu sync.Mutex
	owner    func()
)

// Release gives up the selection we hold from Set. Call it on the way out: an
// xclip orphaned by our exit goes on owning the desktop's clipboard with no
// application behind it.
func (c *Clipboard) Release() {
	ownersMu.Lock()
	held := owner
	owner = nil
	ownersMu.Unlock()
	if held != nil {
		held()
		log.Printf("clipboard: released the clipboard selection on shutdown")
	}
}
