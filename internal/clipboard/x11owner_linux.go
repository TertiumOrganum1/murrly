//go:build linux

package clipboard

import (
	"encoding/binary"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Murrly owns the CLIPBOARD selection itself, in this process, instead of
// forking xclip to do it.
//
// xclip does not implement the selection protocol — it implements one half of
// it. Whatever target a requestor asks for, it answers with its payload:
//
//	TARGETS    -> TARGETS UTF8_STRING     (only two, no TIMESTAMP)
//	TIMESTAMP  -> "the dictated text..."  (must be INTEGER/32)
//	MULTIPLE   -> "the dictated text..."  (must be a list of ATOM_PAIR)
//
// A toolkit that asks for TIMESTAMP — GTK and Chromium both do, and so does
// every clipboard manager — gets text where it expects a number. It cannot
// parse that, so it falls into its retry-and-timeout path, and the paste the
// user pressed sits there for seconds. xclip answers each of those requests in
// 2-3 ms, which is why watching xclip never showed the stall: the delay is
// entirely on the side of whoever believed it.
//
// Owning the selection here fixes that at the source. We advertise a real
// target list, answer TIMESTAMP with the timestamp we took ownership at, and
// service MULTIPLE properly. It also takes the fork/exec off the insert path,
// which was 260-300 ms of every dictation.
//
// Reading somebody else's selection happens here too (see fetch), INCR
// transfers included, so the xclip binary is not involved in either direction
// any more.

// ownedTargets is what we advertise in TARGETS, and the set we will answer.
// Order is the usual best-first, so a requestor that takes the first text
// flavour it recognises gets UTF-8.
var ownedTargets = []string{
	"TARGETS",
	"TIMESTAMP",
	"MULTIPLE",
	"UTF8_STRING",
	"text/plain;charset=utf-8",
	"text/plain",
	"STRING",
	"TEXT",
}

// stampTimeout caps the wait for the server timestamp a claim needs. It is one
// round trip to the X server; if that does not come back promptly the display
// has worse problems than our clipboard.
const stampTimeout = 250 * time.Millisecond

// maxPropChunk is how much of the text goes into one ChangeProperty. Requests
// have a length limit and the reply is built by appending, so a long dictation
// is delivered in pieces before the requestor is told the property is ready.
const maxPropChunk = 128 << 10

type xOwner struct {
	conn   *xgb.Conn
	win    xproto.Window
	atom   map[string]xproto.Atom
	stamps chan xproto.Timestamp

	// readMu serialises reads: a conversion lands in one named property on our
	// one window, so two at a time would overwrite each other.
	readMu sync.Mutex
	notify chan xproto.SelectionNotifyEvent
	chunks chan xproto.PropertyNotifyEvent

	mu   sync.Mutex
	gen  uint64
	held map[xproto.Atom]payload
}

type payload struct {
	text  []byte
	stamp xproto.Timestamp
	// gen tells claims apart. The timestamp cannot: X time is in
	// milliseconds, so two dictations published back to back get the same one,
	// and then releasing the first would pull the second out of the clipboard.
	gen uint64
}

var (
	ownerOnce sync.Once
	theOwner  *xOwner
	ownerErr  error
)

// selectionOwner returns the process-wide owner, connecting on first use.
// Without a display there is no clipboard route at all — the hotkeys and the
// paste chord are X too — so the error travels up rather than being papered
// over.
func selectionOwner() (*xOwner, error) {
	ownerOnce.Do(func() { theOwner, ownerErr = newXOwner() })
	return theOwner, ownerErr
}

func newXOwner() (*xOwner, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("clipboard: cannot reach the X display: %w", err)
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	win, err := xproto.NewWindowId(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// An unmapped InputOnly window: it exists only to be a selection owner and
	// to carry the property we bounce off the server for a timestamp.
	if err := xproto.CreateWindowChecked(conn, 0, win, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOnly, 0,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		conn.Close()
		return nil, err
	}

	o := &xOwner{
		conn:   conn,
		win:    win,
		atom:   make(map[string]xproto.Atom),
		stamps: make(chan xproto.Timestamp, 1),
		notify: make(chan xproto.SelectionNotifyEvent, 1),
		chunks: make(chan xproto.PropertyNotifyEvent, 64),
		held:   make(map[xproto.Atom]payload),
	}
	names := append([]string{
		"CLIPBOARD", "PRIMARY", "SECONDARY", "ATOM_PAIR", "MURRLY_STAMP",
		"MURRLY_READ", "INCR",
	}, ownedTargets...)
	for _, name := range names {
		reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			conn.Close()
			return nil, err
		}
		o.atom[name] = reply.Atom
	}
	go o.serve()
	return o, nil
}

// selectionAtom maps the names the rest of the package uses onto X atoms.
// PRIMARY and SECONDARY are predefined; the tests publish on SECONDARY rather
// than trample the clipboard of whoever is running them.
func (o *xOwner) selectionAtom(selection string) (xproto.Atom, bool) {
	switch selection {
	case "clipboard":
		return o.atom["CLIPBOARD"], true
	case "primary":
		return xproto.AtomPrimary, true
	case "secondary":
		return xproto.AtomSecondary, true
	}
	return 0, false
}

// claim makes text the content of a selection and returns the func that gives
// the selection back.
func (o *xOwner) claim(selection, text string) (func(), error) {
	sel, ok := o.selectionAtom(selection)
	if !ok {
		return nil, fmt.Errorf("clipboard: unknown selection %q", selection)
	}
	stamp, err := o.serverTime()
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	o.gen++
	gen := o.gen
	o.held[sel] = payload{text: []byte(text), stamp: stamp, gen: gen}
	o.mu.Unlock()

	if err := xproto.SetSelectionOwnerChecked(o.conn, o.win, sel, stamp).Check(); err != nil {
		o.mu.Lock()
		if cur, ok := o.held[sel]; ok && cur.gen == gen {
			delete(o.held, sel)
		}
		o.mu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { o.drop(sel, gen) }) }, nil
}

// drop gives a selection back, but only if what we are holding is still the
// payload this release belongs to. A later claim supersedes an earlier one, and
// its release must not pull the newer text out of the clipboard.
func (o *xOwner) drop(sel xproto.Atom, gen uint64) {
	o.mu.Lock()
	cur, ok := o.held[sel]
	if !ok || cur.gen != gen {
		o.mu.Unlock()
		return
	}
	delete(o.held, sel)
	o.mu.Unlock()
	// None as the owner: the desktop's clipboard manager takes the content
	// over from here, exactly as it did when our xclip was killed.
	xproto.SetSelectionOwner(o.conn, xproto.WindowNone, sel, cur.stamp)
}

// serverTime gets a current timestamp from the server. SetSelectionOwner with
// CurrentTime is not allowed to be used for this — the server compares the time
// against the selection's last change, and a stale one is silently ignored,
// leaving us thinking we own a selection we do not. The standard way to get one
// is to append nothing to a property of our own window and read the time off
// the PropertyNotify that comes back.
func (o *xOwner) serverTime() (xproto.Timestamp, error) {
	select {
	case <-o.stamps: // discard anything stale before asking
	default:
	}
	if err := xproto.ChangePropertyChecked(o.conn, xproto.PropModeAppend, o.win,
		o.atom["MURRLY_STAMP"], xproto.AtomString, 8, 0, nil).Check(); err != nil {
		return 0, err
	}
	select {
	case ts := <-o.stamps:
		return ts, nil
	case <-time.After(stampTimeout):
		return 0, fmt.Errorf("clipboard: the X server did not answer within %v", stampTimeout)
	}
}

// serve is the event loop. Everything that touches the connection's event
// stream happens here; requests themselves are safe to issue from anywhere,
// which is why claim does not have to hand work over.
func (o *xOwner) serve() {
	for {
		ev, err := o.conn.WaitForEvent()
		if ev == nil && err == nil {
			// Nothing to fall back to: we ARE the selection owner, and the
			// text we were serving dies with the connection. Say so plainly
			// rather than leaving a silent stop in the log.
			log.Printf("clipboard: the X connection closed; the clipboard selection is no longer served")
			return
		}
		switch e := ev.(type) {
		case xproto.PropertyNotifyEvent:
			switch e.Atom {
			case o.atom["MURRLY_STAMP"]:
				select {
				case o.stamps <- e.Time:
				default:
				}
			case o.atom["MURRLY_READ"]:
				// An INCR transfer delivers its pieces by appending to this
				// property; NewValue means the next one is there.
				if e.State == xproto.PropertyNewValue {
					select {
					case o.chunks <- e:
					default:
					}
				}
			}
		case xproto.SelectionNotifyEvent:
			select {
			case o.notify <- e:
			default:
			}
		case xproto.SelectionRequestEvent:
			o.answer(e)
		case xproto.SelectionClearEvent:
			// Somebody else copied something: the text is theirs now.
			o.mu.Lock()
			delete(o.held, e.Selection)
			o.mu.Unlock()
		}
	}
}

// answer services one conversion request and tells the requestor how it went.
// A target we do not support is refused with a None property, which is what
// lets the requestor move on to its next choice instead of waiting.
func (o *xOwner) answer(req xproto.SelectionRequestEvent) {
	started := time.Now()
	o.mu.Lock()
	p, have := o.held[req.Selection]
	o.mu.Unlock()

	prop := req.Property
	if prop == 0 {
		// Obsolete clients leave the property out and expect the target name
		// to be used instead.
		prop = req.Target
	}
	if !have || !o.put(req.Requestor, prop, req.Target, p) {
		prop = 0
	}
	notify := xproto.SelectionNotifyEvent{
		Time:      req.Time,
		Requestor: req.Requestor,
		Selection: req.Selection,
		Target:    req.Target,
		Property:  prop,
	}
	xproto.SendEvent(o.conn, false, req.Requestor, 0, string(notify.Bytes()))
	o.trace(req, p, have, prop != 0, time.Since(started))
}

// trace is the half of a paste that used to be invisible. A Ctrl+V is three
// things in a row — the key reaches the application, the application asks the
// owner for the text, the owner answers — and when the paste takes seconds only
// one of them is to blame. Owning the selection in-process means the middle one
// is now timestamped: the gap between the keypress and this line is the
// application taking its time to ask, and the duration is ours.
//
// Volume is a handful of lines per paste, and only while a dictation is what is
// in the clipboard.
func (o *xOwner) trace(req xproto.SelectionRequestEvent, p payload, have, served bool, took time.Duration) {
	outcome := "refused"
	if served {
		outcome = "served"
	}
	// req.Time is the server timestamp of the event that caused the paste —
	// the keypress itself. Measured from the moment we published, it says when
	// the user's finger came down; the wall clock on this line says when the
	// application got round to asking. The gap between the two is the
	// application's own lag, and nothing to do with the clipboard.
	since := "key time unknown"
	if have && req.Time != 0 {
		since = fmt.Sprintf("key event %v after we published",
			(time.Duration(req.Time-p.stamp) * time.Millisecond).Round(time.Millisecond))
	}
	log.Printf("clipboard: %s %s to window 0x%x in %v at %s (%s)",
		outcome, o.atomName(req.Target), req.Requestor, took.Round(time.Microsecond),
		time.Now().Format("15:04:05.000"), since)
}

// atomName is for the log: a requestor can ask for anything, including targets
// we never interned, so unknown atoms are resolved from the server.
func (o *xOwner) atomName(a xproto.Atom) string {
	for name, known := range o.atom {
		if known == a {
			return name
		}
	}
	reply, err := xproto.GetAtomName(o.conn, a).Reply()
	if err != nil {
		return fmt.Sprintf("atom %d", a)
	}
	return reply.Name
}

// put writes one target into a property on the requestor's window. Reports
// whether the target was something we can produce.
func (o *xOwner) put(requestor xproto.Window, prop, target xproto.Atom, p payload) bool {
	switch target {
	case o.atom["TARGETS"]:
		data := make([]byte, 0, len(ownedTargets)*4)
		for _, name := range ownedTargets {
			data = binary.LittleEndian.AppendUint32(data, uint32(o.atom[name]))
		}
		return o.write(requestor, prop, xproto.AtomAtom, 32, data)
	case o.atom["TIMESTAMP"]:
		// The thing xclip got wrong: this is a number, not the text.
		return o.write(requestor, prop,
			xproto.AtomInteger, 32, binary.LittleEndian.AppendUint32(nil, uint32(p.stamp)))
	case o.atom["MULTIPLE"]:
		return o.multiple(requestor, prop, p)
	case o.atom["UTF8_STRING"], o.atom["text/plain;charset=utf-8"], o.atom["text/plain"],
		o.atom["STRING"], o.atom["TEXT"], xproto.AtomString:
		return o.write(requestor, prop, target, 8, p.text)
	}
	return false
}

// multiple services a batch request: the requestor has left a list of
// (target, property) pairs on its own window for us to fill in, and a pair we
// cannot produce has its property replaced with None.
func (o *xOwner) multiple(requestor xproto.Window, prop xproto.Atom, p payload) bool {
	reply, err := xproto.GetProperty(o.conn, false, requestor, prop,
		o.atom["ATOM_PAIR"], 0, 1<<16).Reply()
	if err != nil || reply.Format != 32 || len(reply.Value)%8 != 0 {
		return false
	}
	pairs := make([]byte, len(reply.Value))
	copy(pairs, reply.Value)
	for i := 0; i+8 <= len(pairs); i += 8 {
		target := xproto.Atom(binary.LittleEndian.Uint32(pairs[i:]))
		into := xproto.Atom(binary.LittleEndian.Uint32(pairs[i+4:]))
		if into == 0 || !o.put(requestor, into, target, p) {
			binary.LittleEndian.PutUint32(pairs[i+4:], 0)
		}
	}
	return o.write(requestor, prop, o.atom["ATOM_PAIR"], 32, pairs)
}

// write puts bytes into a property, in chunks small enough to fit a request.
// Errors are swallowed on purpose: the usual cause is a requestor that died
// between asking and being answered, and that is not our problem to report.
func (o *xOwner) write(requestor xproto.Window, prop, typ xproto.Atom, format byte, data []byte) bool {
	unit := int(format) / 8
	mode := byte(xproto.PropModeReplace)
	for first := true; first || len(data) > 0; first = false {
		chunk := data
		if len(chunk) > maxPropChunk {
			chunk = chunk[:maxPropChunk-maxPropChunk%unit]
		}
		if err := xproto.ChangePropertyChecked(o.conn, mode, requestor, prop, typ, format,
			uint32(len(chunk)/unit), chunk).Check(); err != nil {
			return false
		}
		data = data[len(chunk):]
		mode = byte(xproto.PropModeAppend)
	}
	return true
}

// readTimeout caps a conversion of somebody else's selection. The read leaves
// the process and is answered by whatever owns the clipboard, so a hung owner
// must cost the dictation nothing: the snapshot is a courtesy, and the
// recording it runs alongside must not wait for it.
const readTimeout = 400 * time.Millisecond

// fetch converts a selection to a target and returns the bytes, the way an
// application does at paste time. This is the reading half that used to be a
// fork of `xclip -o`.
//
// Serialised: the reply lands in one property on our one window.
func (o *xOwner) fetch(selection, target string) ([]byte, bool) {
	sel, ok := o.selectionAtom(selection)
	if !ok {
		return nil, false
	}
	into, err := o.intern(target)
	if err != nil {
		return nil, false
	}
	o.readMu.Lock()
	defer o.readMu.Unlock()

	prop := o.atom["MURRLY_READ"]
	// Start from a clean property, and drain anything the previous read left
	// in the event channels.
	xproto.DeleteProperty(o.conn, o.win, prop)
	o.drain()

	deadline := time.After(readTimeout)
	if err := xproto.ConvertSelectionChecked(o.conn, o.win, sel, into, prop,
		xproto.TimeCurrentTime).Check(); err != nil {
		return nil, false
	}
	select {
	case ev := <-o.notify:
		// Property None is the owner saying it cannot produce this target.
		if ev.Property == 0 {
			return nil, false
		}
	case <-deadline:
		log.Printf("clipboard: the owner did not answer %s within %v", target, readTimeout)
		return nil, false
	}

	reply, err := xproto.GetProperty(o.conn, true, o.win, prop,
		xproto.GetPropertyTypeAny, 0, maxPropChunk/4).Reply()
	if err != nil {
		return nil, false
	}
	if reply.Type != o.atom["INCR"] {
		if reply.BytesAfter == 0 {
			return reply.Value, true
		}
		return o.rest(prop, reply.Value)
	}
	return o.incremental(prop, deadline)
}

// rest picks up what did not fit in the first GetProperty. The property was
// deleted by that read, so the remainder is fetched by offset from a fresh one.
func (o *xOwner) rest(prop xproto.Atom, first []byte) ([]byte, bool) {
	out := append([]byte(nil), first...)
	for offset := uint32(len(first)) / 4; ; {
		reply, err := xproto.GetProperty(o.conn, false, o.win, prop,
			xproto.GetPropertyTypeAny, offset, maxPropChunk/4).Reply()
		if err != nil || len(reply.Value) == 0 {
			xproto.DeleteProperty(o.conn, o.win, prop)
			return out, len(out) > 0
		}
		out = append(out, reply.Value...)
		if reply.BytesAfter == 0 {
			xproto.DeleteProperty(o.conn, o.win, prop)
			return out, true
		}
		offset += uint32(len(reply.Value)) / 4
	}
}

// incremental runs an INCR transfer: the owner has told us the size is coming
// in pieces, and each delete of the property is the signal to send the next.
// Anything big — a screenshot, a long document — arrives this way, which is
// why reading a selection is not simply a GetProperty.
func (o *xOwner) incremental(prop xproto.Atom, deadline <-chan time.Time) ([]byte, bool) {
	var out []byte
	// Deleting the property is what tells the owner to send the first piece.
	xproto.DeleteProperty(o.conn, o.win, prop)
	for {
		select {
		case <-o.chunks:
		case <-deadline:
			log.Printf("clipboard: the owner stalled partway through an INCR transfer")
			return nil, false
		}
		reply, err := xproto.GetProperty(o.conn, true, o.win, prop,
			xproto.GetPropertyTypeAny, 0, maxPropChunk/4).Reply()
		if err != nil {
			return nil, false
		}
		// A zero-length piece ends the transfer.
		if len(reply.Value) == 0 && reply.BytesAfter == 0 {
			return out, len(out) > 0
		}
		out = append(out, reply.Value...)
		if reply.BytesAfter > 0 {
			more, ok := o.rest(prop, nil)
			if !ok {
				return nil, false
			}
			out = append(out, more...)
		}
		if len(out) > maxStashedImage {
			log.Printf("clipboard: the clipboard holds more than %d MB; not keeping it", maxStashedImage>>20)
			return nil, false
		}
	}
}

// drain clears events left over from a read that timed out, so a late reply
// is not mistaken for the answer to the next one.
func (o *xOwner) drain() {
	for {
		select {
		case <-o.notify:
		case <-o.chunks:
		default:
			return
		}
	}
}

// intern resolves a target name to an atom, remembering it. Targets come from
// TARGETS listings, so the set is open-ended.
func (o *xOwner) intern(name string) (xproto.Atom, error) {
	o.mu.Lock()
	if a, ok := o.atom[name]; ok {
		o.mu.Unlock()
		return a, nil
	}
	o.mu.Unlock()
	reply, err := xproto.InternAtom(o.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, err
	}
	o.mu.Lock()
	o.atom[name] = reply.Atom
	o.mu.Unlock()
	return reply.Atom, nil
}
