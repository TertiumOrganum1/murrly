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

	// macOS Privacy panes (System Settings deep-links). Non-nil only
	// on darwin; renderers must hide the Permissions submenu when both
	// are nil (Linux has no TCC-style permission gate to surface).
	OnOpenMicSettings   func()
	OnOpenAccessibility func()

	OnQuit func()
}
