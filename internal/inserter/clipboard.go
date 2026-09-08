package inserter

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

// ClipboardBackend is the platform clipboard. Save returns an opaque
// snapshot handed back to Restore; the route never introspects it.
type ClipboardBackend interface {
	Save() (any, error)
	Set(string) error
	Restore(any) error
}

// FetchReporter is implemented by backends that can say when their
// publication was read. Used only to report timings: choosing how long to
// hold the clipboard is a judgement about real applications, and this is
// the measurement it should be based on.
type FetchReporter interface {
	FetchTimes() []time.Time
}

// consumerReadWait bounds how long we wait, after the paste key, for the
// target to read what we published.
//
// Finding where that read happens took three wrong answers. It is not the
// ownership change (that is the application refreshing its cache), and it
// is not while the modifier is held — waiting there saw nothing and cost
// 400 ms on every insert. It happens at the paste key itself, within a few
// tens of milliseconds, so the mark goes down with the modifier and the
// wait comes after the chord.
const consumerReadWait = 500 * time.Millisecond

// The wait before the chord adapts to the machine instead of being a
// number chosen once and hoped for.
//
// The failure it guards against is load-dependent: a busy desktop (the GPU
// saturated by the transcription that just finished, for instance) leaves
// the target application's event loop lagging, so it has not yet processed
// the clipboard ownership change when the paste key arrives — and then it
// pastes its own cache without reading anything. A fixed delay can only be
// tuned for one machine on one day; this one grows whenever a paste is
// observed to have read nothing, and eases back down while pastes keep
// working.
var (
	adaptiveWait   atomic.Int64 // current wait, nanoseconds
	goodPastes     atomic.Int64
	maxPrePaste    = 2500 * time.Millisecond
	prePasteStep   = 300 * time.Millisecond
	prePasteRelief = 100 * time.Millisecond
	// reliefAfter is how many clean pastes in a row it takes before the
	// wait shrinks: slow to relax, quick to back off.
	reliefAfter = int64(10)
)

// currentPrePaste returns the wait to use before the next chord.
func currentPrePaste() time.Duration {
	if d := adaptiveWait.Load(); d > 0 {
		return time.Duration(d)
	}
	return prePasteDelay
}

// notePasteOutcome feeds the adaptation. read=false means the target never
// looked at what we published — the signature of the stale paste.
func notePasteOutcome(read bool) {
	cur := currentPrePaste()
	if !read {
		// Deliberately NOT raising the wait any more. Growing it was tried
		// and disproved on the spot: 600 ms, 900 ms and 1.2 s in three
		// consecutive pastes, every one of them read nothing. The target
		// in that state does not consult the clipboard at all — not when
		// ownership changes, not when the paste key arrives — so no amount
		// of waiting reaches it. All that is left is to say so.
		goodPastes.Store(0)
		log.Printf("clipboard: the target pasted from its own cache and never looked at ours — " +
			"this route cannot deliver into that application; direct input can")
		return
	}
	if goodPastes.Add(1) < reliefAfter {
		return
	}
	goodPastes.Store(0)
	next := cur - prePasteRelief
	if next < prePasteDelay {
		next = prePasteDelay
	}
	if next != cur {
		log.Printf("clipboard: %d clean pastes — easing the pre-paste wait %v → %v", reliefAfter, cur, next)
		adaptiveWait.Store(int64(next))
	}
}

// lateReadWait is how long to keep the text published when the paste key
// did not produce a read.
//
// The target reads the clipboard lazily. When that read lands after we
// have already put the user's content back, the application caches the OLD
// text — and every later paste that skips reading serves that stale cache,
// which is why a single miss turns into a run of wrong pastes. So a miss
// is met with patience rather than a quick restore: whatever reads next
// gets the dictation, and the cache is poisoned with the right thing.
const lateReadWait = 3 * time.Second

// postReadHold is the margin kept after the read was seen. Short on
// purpose: the read was the whole point of waiting, and what remains is
// time the user's own clipboard stays displaced.
const postReadHold = 150 * time.Millisecond

// ClipboardConfirmer is implemented by backends that can re-check their own
// ownership of the clipboard. Used right before the paste chord: if
// something claimed the selection back in the meantime, pressing Ctrl+V
// would paste that instead of the dictation.
type ClipboardConfirmer interface {
	// ServesText reports whether the clipboard currently serves exactly
	// this text, i.e. our own publication is still the live one.
	ServesText(text string) bool
}

// PasterBackend synthesises the paste chord. beforeKey is called at the
// last moment before the keys go down.
type PasterBackend interface {
	Paste(beforeKey func()) error
}

// prePasteDelay sits between publishing the text and pressing the paste
// chord.
//
// It exists because Chromium/Electron applications do not read the
// selection when you paste — they keep a cached copy and refresh it when
// the X server tells them ownership changed. Press the chord before that
// notification has been processed and the application pastes its stale
// cache: the user's OLD clipboard instead of the dictation.
//
// The refresh is no longer unobservable — FetchTimes records it — and the
// logs say it lands at publish + 0 ms: on healthy inserts the first read
// sits at the very start of the publish→chord window (-984 ms out of a
// 984 ms window, three times running). So 600 ms was paying for a wait the
// application had already finished, at the cost of the pause the user
// feels on every dictation. What remains is a margin, not a guess.
const prePasteDelay = 250 * time.Millisecond

// postPasteDelay is how long the text stays in the clipboard after the
// chord, before the user's own content goes back.
//
// It is insurance, not the load-bearing part. Measured against a live
// Electron application, every read of the selection happened BEFORE the
// chord — the last one 53 ms before it — and none after: the application
// pastes from the cache above and never asks again. An earlier version
// waited for a read AFTER the chord before restoring, which in such an
// application is an event that never arrives; it timed out on every single
// insert. This window is here for the applications that DO read lazily
// (GTK dialogs, terminals), and is kept short because the user's own
// clipboard is displaced for exactly this long — long enough and a Ctrl+V
// right after dictating hands them the dictation back.
const postPasteDelay = 300 * time.Millisecond

// Clipboard is the legacy route: borrow the clipboard, paste, give it back.
// Kept as the last fallback for fields that expose neither an accessible
// object nor a working keyboard path.
type Clipboard struct {
	CB         ClipboardBackend
	Paster     PasterBackend
	PasteDelay time.Duration
	// Replace switches the route to its blunt form: publish the text, press
	// the chord, done. The user's clipboard is not snapshotted and not put
	// back, and nothing waits to see whether the target read anything.
	//
	// It is the answer to the applications this route cannot serve honestly.
	// Chromium/Electron do not query the selection owner on Ctrl+V — they
	// paste a cached copy refreshed on the ownership-change notification —
	// so the read we would time the restore against never arrives, the wait
	// runs to its full timeout on every insert, and the eventual lazy read
	// lands AFTER the restore and pastes the user's old clipboard. Not
	// restoring removes the race outright: whatever reads the selection,
	// early or late, finds the dictation.
	//
	// The price is that dictating costs the user their clipboard, which is
	// why this is a choice and not the default.
	Replace bool

	// OnDisplaced receives the clipboard snapshot taken immediately before
	// the replacing route overwrites it — the opaque value the backend's
	// Save returned, unexamined here as everywhere else on this route. The
	// caller keeps it somewhere the user can ask for it back; nil means
	// nobody is collecting and the snapshot is skipped entirely.
	//
	// This is not the restore that the preserving mode does. Nothing is put
	// back on its own and no timing depends on it: the snapshot is taken
	// before the paste, under displacedSnapshotWait, and an owner that does
	// not answer in that time costs the insert nothing.
	OnDisplaced func(any)
}

// displacedSnapshotWait bounds the courtesy read of the clipboard that the
// replacing route is about to overwrite. Short on purpose — reading a
// selection means a round trip to whatever process owns it, and the whole
// argument for the replacing route is that it never blocks on one.
const displacedSnapshotWait = 300 * time.Millisecond

// snapshotBackend is a ClipboardBackend that can bound its own read. Linux
// implements it because there the read leaves the process; the platforms
// where it does not are served by the plain Save below.
type snapshotBackend interface {
	SaveWithin(time.Duration) (any, error)
}

// stashDisplaced hands the current clipboard to OnDisplaced before it is
// overwritten. Every failure here is silent by design: the snapshot is a
// convenience, and the dictation must land either way.
func (c *Clipboard) stashDisplaced() {
	if c.OnDisplaced == nil {
		return
	}
	save := c.CB.Save
	if sb, ok := c.CB.(snapshotBackend); ok {
		save = func() (any, error) { return sb.SaveWithin(displacedSnapshotWait) }
	}
	prev, err := save()
	if err != nil {
		log.Printf("clipboard: could not snapshot the clipboard before overwriting it: %v", err)
		return
	}
	c.OnDisplaced(prev)
}

func (c *Clipboard) Name() string { return "clipboard" }

// insertReplacing is the whole of the replacing mode: publish, press the
// chord, done — no waiting for anything unobservable, and nothing put back.
// The one read it does make, the snapshot for the menu, is bounded so short
// that a hung selection owner cannot stall the insert before it starts.
func (c *Clipboard) insertReplacing(text string) error {
	c.stashDisplaced()
	if err := c.CB.Set(text); err != nil {
		return fmt.Errorf("clipboard.Set: %w", err)
	}
	// Still needed: an application that has not processed the ownership
	// change yet pastes its own cache. This margin is the only wait left.
	time.Sleep(prePasteDelay)
	if err := c.Paster.Paste(func() {}); err != nil {
		return fmt.Errorf("paster.Paste: %w", err)
	}
	return nil
}

func (c *Clipboard) Insert(text string) error {
	if text == "" {
		return nil
	}
	if c.Replace {
		return c.insertReplacing(text)
	}
	saved, err := c.CB.Save()
	if err != nil {
		return fmt.Errorf("clipboard.Save: %w", err)
	}
	// From here on the user's clipboard is displaced, so every exit path
	// below has to put it back.
	restore := func() {
		if err := c.CB.Restore(saved); err != nil {
			log.Printf("clipboard.Restore: %v", err)
		}
	}
	if err := c.CB.Set(text); err != nil {
		restore()
		return fmt.Errorf("clipboard.Set: %w", err)
	}
	published := time.Now()
	preWait := currentPrePaste()
	// Which window is focused when the text is published, and which one is
	// focused when the key arrives. Chromium-based applications only track
	// clipboard changes while focused, so publishing into an unfocused
	// target — our own recording overlay still up, say — means the change
	// is never noticed and the paste comes out of a stale cache without
	// ever touching the clipboard. That is the failure signature we keep
	// hitting, and this is how to confirm or kill the theory.
	winAtPublish := activeWindowID()
	// Let the target application notice the new clipboard before the chord.
	// This is the load-bearing wait: an application that has not processed
	// the ownership change yet does not merely read stale text — it does
	// not read at all, because it believes its own cache is current. That
	// failure was measured with 655 ms between publishing and the chord,
	// so the value here is deliberately generous; the paste itself puts in
	// text of any length instantly, which is the whole point of this route.
	time.Sleep(preWait)
	// And check we are still the one serving it: a clipboard manager or
	// another application can claim the selection in that window, and
	// pasting then would deliver their content, not the dictation.
	if conf, ok := c.CB.(ClipboardConfirmer); ok && !conf.ServesText(text) {
		log.Printf("insert: lost the clipboard before pasting; republishing")
		if err := c.CB.Set(text); err != nil {
			restore()
			return fmt.Errorf("clipboard.Set (republish): %w", err)
		}
		time.Sleep(preWait)
	}

	// Mark the read counter as the modifier goes down, so the read the
	// paste key triggers is attributable to THIS paste and not to an
	// earlier cache refresh.
	rep, canReport := c.CB.(FetchReporter)
	base := 0
	if err := c.Paster.Paste(func() {
		if canReport {
			base = len(rep.FetchTimes())
		}
	}); err != nil {
		restore()
		return fmt.Errorf("paster.Paste: %w", err)
	}
	chord := time.Now()
	winAtChord := activeWindowID()

	hold := postPasteDelay
	if c.PasteDelay > hold {
		hold = c.PasteDelay
	}
	read := false
	if canReport {
		// Wait for the target to read what we published. First the quick
		// window, which is where a healthy paste lands.
		waitFor := func(limit time.Duration) bool {
			for time.Since(chord) < limit {
				if len(rep.FetchTimes()) > base {
					return true
				}
				time.Sleep(5 * time.Millisecond)
			}
			return false
		}
		read = waitFor(consumerReadWait)
		if !read {
			// Missed. Do NOT restore now: the target reads lazily, and a
			// read that lands after the restore caches the user's old
			// clipboard — which every later paste that skips reading then
			// serves, turning one miss into a run of wrong pastes. Keep
			// the dictation published until something reads it.
			if read = waitFor(lateReadWait); read {
				log.Printf("clipboard: late read at +%v — held the text until it arrived",
					time.Since(chord).Round(time.Millisecond))
			}
		}
		if read {
			// The target has the text. Short margin, then hand the
			// clipboard back — the rest is time the user's own content
			// stays displaced.
			time.Sleep(postReadHold)
		} else {
			// Nothing read it in three seconds. Two very different causes
			// look identical from here: either the target really pasted
			// from its own cache, or somebody took the selection from us
			// and served it instead — which is exactly how the user's old
			// clipboard ends up pasted.
			stillOurs := true
			if conf, ok := c.CB.(ClipboardConfirmer); ok {
				stillOurs = conf.ServesText(text)
			}
			if stillOurs {
				log.Printf("clipboard: nothing read our text in %v and we still own the selection — "+
					"the target pasted from its own cache", lateReadWait)
			} else {
				log.Printf("clipboard: WE LOST THE SELECTION — another owner served the target, " +
					"which is why the old clipboard appeared")
			}
		}
		var offsets []string
		for _, t := range rep.FetchTimes() {
			offsets = append(offsets, t.Sub(chord).Round(time.Millisecond).String())
		}
		notePasteOutcome(read)
		log.Printf("clipboard: read=%v; focus same=%v; %v publish→chord; displaced %v after it; reads: %s",
			read, winAtPublish == winAtChord, chord.Sub(published).Round(time.Millisecond),
			time.Since(chord).Round(time.Millisecond), strings.Join(offsets, " "))
	} else {
		time.Sleep(hold)
	}
	restore()
	return nil
}
