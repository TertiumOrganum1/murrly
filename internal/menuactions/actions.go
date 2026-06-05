// Package menuactions defines the platform-neutral set of menu actions
// that Murrly's tray (Linux, macOS) and Dock right-click menu (macOS
// only) both render. Both renderers consume the same struct so adding
// or renaming an item is a single edit at the call site; the renderers
// themselves only worry about toolkit-specific glue.
package menuactions

// Actions is the full set of behaviors a Murrly menu surfaces.
//
// Callbacks may be nil — each renderer skips items whose callback is
// nil. This is how the Permissions submenu is hidden on Linux: main
// leaves OnOpenMicSettings / OnOpenAccessibility unset there.
type Actions struct {
	// Recent transcripts. Index 0 = most recent, up to N-1.
	OnCopyTranscript func(index int)

	// Whisper model picker. ModelLabels seed the submenu; clicking
	// item i fires OnPickModel(i). ActiveModelIndex tells the renderer
	// which row to render with a check mark on first paint (-1 if none).
	OnPickModel      func(index int)
	ModelLabels      []string
	ActiveModelIndex int

	// Scoring-mode picker (multi-inference only). Same flat-checkable
	// shape as the model picker: ScoringLabels seed the items, clicking
	// item i fires OnPickScoringMode(i), ActiveScoringIndex is the row
	// checked on first paint. Left nil / empty in single-pass mode and on
	// platforms without multi-inference, so the renderer omits the group.
	OnPickScoringMode  func(index int)
	ScoringLabels      []string
	ActiveScoringIndex int

	// OnReprocess re-runs the last recorded audio through the
	// transcriber with a small silence prefix that perturbs the
	// decoder's chunk-boundary alignment — same trick as the
	// auto-retry, but triggered manually when the user sees a bad
	// transcription. Wired through the App's event channel so the
	// menu callback returns immediately and the actual work happens
	// on the App's goroutine (avoids racing with F12 flow).
	OnReprocess func()

	// Config helpers.
	OnReloadConfig func()
	OnOpenConfig   func()

	// Autostart toggle. The callback flips the underlying state and
	// returns the new value so each renderer can sync its own check
	// mark without a separate round-trip. IsAutostartOn reports the
	// current state at render time (for the initial check mark).
	OnToggleAutostart func() bool
	IsAutostartOn     func() bool

	// PadSilence toggle ("Тишина по краям"). Same shape as
	// autostart: OnTogglePadSilence flips and returns the new state;
	// IsPadSilenceOn reports the initial value at render time.
	OnTogglePadSilence func() bool
	IsPadSilenceOn     func() bool

	// Multi-inference toggle ("Множественное распознавание"). Same shape
	// as autostart: OnToggleMulti flips the live state and returns the new
	// value; IsMultiOn reports the initial value at render time. Left nil
	// when no multi-inference engine is built (single-pass / count == 1),
	// so the renderer omits the item.
	OnToggleMulti func() bool
	IsMultiOn     func() bool

	// macOS Privacy panes (System Settings deep-links). Non-nil only
	// on darwin; renderers must hide the Permissions submenu when both
	// are nil (Linux has no TCC-style permission gate to surface).
	OnOpenMicSettings   func()
	OnOpenAccessibility func()

	OnQuit func()
}
