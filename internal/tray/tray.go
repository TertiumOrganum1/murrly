// Package tray displays the systray icon and exposes the user actions.
package tray

import (
	"os"
	"os/exec"

	"fyne.io/systray"
)

type State int

const (
	StateIdle State = iota
	StateRecording
	StateTranscribing
	StateError
)

type Actions struct {
	OnPauseToggle func()
	OnReload      func()
	OnOpenConfig  func()
	OnQuit        func()
}

type Tray struct {
	icons      map[State][]byte
	stateCh    chan State
	actions    Actions
	configPath string
}

func New(icons map[State][]byte, configPath string, actions Actions) *Tray {
	return &Tray{
		icons:      icons,
		stateCh:    make(chan State, 8),
		actions:    actions,
		configPath: configPath,
	}
}

// Run blocks. Call from the main goroutine — systray must own the main thread
// on macOS, and is safe on Linux.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) Quit() {
	systray.Quit()
}

func (t *Tray) SetState(s State) {
	select {
	case t.stateCh <- s:
	default:
	}
}

func (t *Tray) onReady() {
	systray.SetTitle("voice-input")
	systray.SetTooltip("voice-input: idle")
	if icon, ok := t.icons[StateIdle]; ok {
		systray.SetIcon(icon)
	}

	pauseItem := systray.AddMenuItem("Pause", "Stop listening for hotkey")
	reloadItem := systray.AddMenuItem("Reload config", "Reload config from disk")
	openCfgItem := systray.AddMenuItem("Open config file", "Open config.toml in $EDITOR or xdg-open")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Stop voice-input")

	go func() {
		for {
			select {
			case s := <-t.stateCh:
				if icon, ok := t.icons[s]; ok {
					systray.SetIcon(icon)
				}
				systray.SetTooltip("voice-input: " + stateName(s))
			case <-pauseItem.ClickedCh:
				if t.actions.OnPauseToggle != nil {
					t.actions.OnPauseToggle()
				}
			case <-reloadItem.ClickedCh:
				if t.actions.OnReload != nil {
					t.actions.OnReload()
				}
			case <-openCfgItem.ClickedCh:
				openInEditor(t.configPath)
			case <-quitItem.ClickedCh:
				if t.actions.OnQuit != nil {
					t.actions.OnQuit()
				}
				t.Quit()
				return
			}
		}
	}()
}

func (t *Tray) onExit() {}

func stateName(s State) string {
	switch s {
	case StateRecording:
		return "recording"
	case StateTranscribing:
		return "transcribing"
	case StateError:
		return "error"
	default:
		return "idle"
	}
}

func openInEditor(path string) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	var cmd *exec.Cmd
	if editor != "" {
		cmd = exec.Command(editor, path)
	} else {
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}
