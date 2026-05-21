// Package tray displays the systray icon and exposes the user actions.
package tray

import (
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

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
	OnPauseToggle    func()
	OnReload         func()
	OnOpenConfig     func()
	OnCopyTranscript func(index int)
	OnQuit           func()
}

type Tray struct {
	icons        map[State][]byte
	stateCh      chan State
	transcriptCh chan []string
	actions      Actions
	configPath   string
}

func New(icons map[State][]byte, configPath string, actions Actions) *Tray {
	return &Tray{
		icons:        icons,
		stateCh:      make(chan State, 8),
		transcriptCh: make(chan []string, 8),
		actions:      actions,
		configPath:   configPath,
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

func (t *Tray) SetRecentTranscripts(items []string) {
	copyItems := make([]string, len(items))
	copy(copyItems, items)
	select {
	case t.transcriptCh <- copyItems:
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
	copyLatestItem := systray.AddMenuItem("Copy latest recognized text", "Copy the latest recognized text to clipboard")
	copyPreviousItem := systray.AddMenuItem("Copy previous recognized text", "Copy the previous recognized text to clipboard")
	copyOlderItem := systray.AddMenuItem("Copy older recognized text", "Copy the third latest recognized text to clipboard")
	copyItems := []*systray.MenuItem{copyLatestItem, copyPreviousItem, copyOlderItem}
	for i, item := range copyItems {
		item.SetTitle(transcriptMenuTitle(i, ""))
		item.Disable()
	}
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
			case items := <-t.transcriptCh:
				updateTranscriptMenuItems(copyItems, items)
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
			case <-copyLatestItem.ClickedCh:
				t.copyTranscript(0)
			case <-copyPreviousItem.ClickedCh:
				t.copyTranscript(1)
			case <-copyOlderItem.ClickedCh:
				t.copyTranscript(2)
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

func (t *Tray) copyTranscript(index int) {
	if t.actions.OnCopyTranscript != nil {
		t.actions.OnCopyTranscript(index)
	}
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

func updateTranscriptMenuItems(menuItems []*systray.MenuItem, transcripts []string) {
	for i, item := range menuItems {
		text := ""
		if i < len(transcripts) {
			text = transcripts[i]
		}
		item.SetTitle(transcriptMenuTitle(i, text))
		if text == "" {
			item.Disable()
		} else {
			item.Enable()
		}
	}
}

func transcriptMenuTitle(index int, text string) string {
	labels := []string{
		"Copy latest",
		"Copy previous",
		"Copy older",
	}
	label := "Copy recognized text"
	if index >= 0 && index < len(labels) {
		label = labels[index]
	}
	if text == "" {
		return label
	}
	return label + ": " + transcriptPreview(text, 48)
}

func transcriptPreview(text string, limit int) string {
	compact := strings.Join(strings.Fields(text), " ")
	if limit <= 0 || utf8.RuneCountInString(compact) <= limit {
		return compact
	}
	runes := []rune(compact)
	return string(runes[:limit]) + "..."
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
