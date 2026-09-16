//go:build linux

package clipboard

import (
	"encoding/binary"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jezek/xgb/xproto"
)

// The X11 clipboard is not a store — it is a process. Whoever "copied" keeps
// the bytes in its own memory and hands them over on request, so text exists
// in the clipboard only for as long as some process owns the selection.
//
// Murrly is that process, in-process: see x11owner_linux.go. Nothing here
// shells out any more, in either direction. xclip is gone from this package
// entirely — it got the owner's half of the selection protocol wrong (TIMESTAMP
// and MULTIPLE came back as the text itself), and every read of it was a
// fork/exec on top.

// snapshotting gates the copy of what the user had in the clipboard before a
// dictation displaced it, and is set from the config (on by default).
//
// It is a gate at all because unlike everything else in this file a snapshot
// is a request made OF another application: it asks whatever owns the
// clipboard to hand its content over, and when that is a busy Electron window
// the answer can take as long as the window feels like taking. That is
// survivable only because of where the call sits — at the start of a
// recording, in its own goroutine, with nothing waiting on it. Keep it there.
//
// The package stays inert until EnableSnapshot says otherwise, so a caller
// that never wires the config gets no surprise reads of somebody else's
// clipboard.
var snapshotting atomic.Bool

// EnableSnapshot turns the previous-clipboard snapshot on, from the config.
func EnableSnapshot(on bool) { snapshotting.Store(on) }

// maxStashedImage caps what a snapshot will hold in RSS. A screenshot of a
// region is a few hundred KB and a full 4K screen a few MB; past this we are
// storing somebody's scanned document for the rest of the session, and the
// point of the stash is undoing an accident, not archiving.
const maxStashedImage = 32 << 20

// imageTargets are the picture formats we will keep, best first. PNG is what
// every screenshot tool on this desktop offers and it is lossless; the others
// are there so a snapshot from an odd source is not dropped on the floor.
var imageTargets = []string{"image/png", "image/webp", "image/jpeg", "image/bmp", "image/tiff"}

// textTargets are the plain-text flavours a selection owner may offer, best
// first. Owners differ on which they advertise — GTK leads with UTF8_STRING,
// older Qt with STRING, browsers with text/plain;charset=utf-8.
var textTargets = []string{"UTF8_STRING", "text/plain;charset=utf-8", "text/plain", "STRING"}

// readSystemSnapshot is the X11 half of Snapshot (see clipboard.go, which
// filters out our own dictations before the caller sees them).
func (c *Clipboard) readSystemSnapshot() (Saved, bool) {
	if !snapshotting.Load() {
		return Saved{}, false
	}
	return readSnapshotFrom("clipboard")
}

// readSnapshotFrom is readSystemSnapshot against a named selection. Split out
// so the tests can exercise the real round-trip on SECONDARY instead of
// trampling the clipboard of whoever is running them.
//
// TARGETS comes first and is not an optimisation — it is the only way to know
// what is in there. Reading blind is how a PNG ends up stashed as a string.
func readSnapshotFrom(selection string) (Saved, bool) {
	o, err := selectionOwner()
	if err != nil {
		return Saved{}, false
	}
	targets := readTargets(o, selection)
	if len(targets) == 0 {
		return Saved{}, false
	}
	// Text wins when both are on offer: it is the thing that can actually go
	// back into the clipboard, and a picture is only ever a file on disk.
	for _, want := range textTargets {
		if hasTarget(targets, want) {
			return readTextFrom(o, selection, targets)
		}
	}
	for _, want := range imageTargets {
		if !hasTarget(targets, want) {
			continue
		}
		out, ok := o.fetch(selection, want)
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
func readTargets(o *xOwner, selection string) []string {
	out, ok := o.fetch(selection, "TARGETS")
	if !ok || len(out)%4 != 0 {
		return nil
	}
	var targets []string
	for i := 0; i+4 <= len(out); i += 4 {
		targets = append(targets, o.atomName(xproto.Atom(binary.LittleEndian.Uint32(out[i:]))))
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

// readTextFrom reads the plain text of a selection, trying only the flavours
// the owner said it has.
func readTextFrom(o *xOwner, selection string, targets []string) (Saved, bool) {
	for _, target := range textTargets {
		if !hasTarget(targets, target) {
			continue
		}
		if out, ok := o.fetch(selection, target); ok && len(out) > 0 {
			return Saved{Text: string(out), HasContent: true}, true
		}
	}
	return Saved{}, false
}

// Publish makes text the content of the clipboard and returns the func that
// gives the clipboard back. The caller owns the selection for exactly as long
// as it holds onto that func, which for the insert route is the paste chord
// plus a short hold.
func (c *Clipboard) Publish(text string) (func(), error) {
	release, err := publishTo("clipboard", text)
	if err == nil {
		markOurs(text)
	}
	return release, err
}

// PublishAndHold publishes text and keeps the selection instead of handing it
// straight back. The dictation stays in the clipboard, served by us, until the
// next publish supersedes it, the user copies something else, or Release runs
// on the way out.
//
// Giving the selection back is not free: it is a second ownership change
// moments after the paste chord, and what picks the content up is the desktop's
// clipboard manager — which this machine's log has twice caught taking more
// than 400 ms to answer a request. Anything that comes for the text after we
// let go waits on that instead of on us.
func (c *Clipboard) PublishAndHold(text string) error {
	release, err := publishTo("clipboard", text)
	if err != nil {
		return err
	}
	markOurs(text)
	ownersMu.Lock()
	prev := owner
	owner = release
	ownersMu.Unlock()
	// The previous holder has already lost the selection to this one; calling
	// its release just tidies up the bookkeeping now rather than at exit.
	if prev != nil {
		prev()
	}
	return nil
}

// publishTo is Publish against a named selection. Split out so the tests can
// exercise the real X round-trip on SECONDARY instead of trampling the
// clipboard of whoever is running them.
func publishTo(selection, text string) (func(), error) {
	o, err := selectionOwner()
	if err != nil {
		return nil, err
	}
	return o.claim(selection, text)
}

// Set puts text in the clipboard and leaves it there — for the tray items and
// the paste-last binding, where the user explicitly asked for something to be
// in the clipboard. Ownership is kept (it has to be: drop it and the text is
// gone) but tracked, so Release can hand it back on the way out.
func (c *Clipboard) Set(text string) error {
	release, err := publishTo("clipboard", text)
	if err != nil {
		return err
	}
	// The user asked for this text to be in the clipboard, so it counts as
	// theirs from now on — see forgetOurs.
	forgetOurs()
	ownersMu.Lock()
	prev := owner
	owner = release
	ownersMu.Unlock()
	if prev != nil {
		prev()
	}
	return nil
}

// owner is the release func for the selection we are currently holding, if
// any. Only the latest matters: claiming the selection supersedes the previous
// claim.
var (
	ownersMu sync.Mutex
	owner    func()
)

// Release gives up the selection on the way out, so the clipboard does not go
// on pointing at a window of a process that has exited.
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
