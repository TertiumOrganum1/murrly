// Package tray displays the systray icon and exposes the user actions.
package tray

import (
	"strings"
	"unicode/utf8"

	"fyne.io/systray"

	"github.com/tertiumorganum1/murrly/internal/menuactions"
)

type State int

const (
	StateIdle State = iota
	StateRecording
	StateTranscribing
	StateError
)

type Tray struct {
	icons         map[State][]byte
	stateCh       chan State
	transcriptCh  chan []string
	activeModelCh chan int
	actions       *menuactions.Actions
}

func New(icons map[State][]byte, actions *menuactions.Actions) *Tray {
	return &Tray{
		icons:         icons,
		stateCh:       make(chan State, 8),
		transcriptCh:  make(chan []string, 8),
		activeModelCh: make(chan int, 8),
		actions:       actions,
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

// SetActiveModel moves the checkmark in the Model submenu to the item at
// the given index. Pass -1 to clear all checkmarks. Called from the
// model-pick callback after a successful hot-swap.
func (t *Tray) SetActiveModel(index int) {
	select {
	case t.activeModelCh <- index:
	default:
	}
}

func (t *Tray) onReady() {
	// No SetTitle — the cat icon alone is the brand mark. A text label
	// next to it just eats menu-bar real estate (especially on M-series
	// macs where every pixel before the notch counts).
	systray.SetTooltip("Murrly: idle")
	if icon, ok := t.icons[StateIdle]; ok {
		systray.SetIcon(icon)
	}

	// Transcript slots first — by far the most common reason to open the
	// menu is to recopy a recent recognition, so it lives at the top.
	copyLatestItem := systray.AddMenuItem("— (последнее)", "Скопировать в буфер обмена")
	copyPreviousItem := systray.AddMenuItem("— (предыдущее)", "Скопировать в буфер обмена")
	copyOlderItem := systray.AddMenuItem("— (ещё раньше)", "Скопировать в буфер обмена")
	copyItems := []*systray.MenuItem{copyLatestItem, copyPreviousItem, copyOlderItem}
	for i, item := range copyItems {
		item.SetTitle(transcriptMenuTitle(i, ""))
		item.Disable()
	}

	// "Reprocess last" — re-runs the most recent recording through
	// Whisper with a small silence prefix. Cheap manual retry when
	// the first decode dropped punctuation or otherwise looks bad.
	reprocessItem := systray.AddMenuItem("Перепроцессить последнее", "Прогнать последнюю запись через Whisper ещё раз (со сдвигом окна)")

	systray.AddSeparator()

	// Hide the model picker when fewer than 2 models are available —
	// there's nothing to pick between.
	var modelItems []*systray.MenuItem
	if len(t.actions.ModelLabels) >= 2 {
		// AddSubMenuItemCheckbox is required on Linux: AddSubMenuItem
		// creates items with toggle-type="" in dbusmenu, which
		// Cinnamon/AppIndicator renders without their label. The
		// checkbox variant also lets Check()/Uncheck() actually move
		// the visible checkmark, which is a no-op on Linux for
		// non-checkbox items (systray.go:172, 211).
		modelHeader := systray.AddMenuItem("Модель", "Выбрать модель Whisper")
		modelItems = make([]*systray.MenuItem, len(t.actions.ModelLabels))
		for i, lbl := range t.actions.ModelLabels {
			checked := i == t.actions.ActiveModelIndex
			modelItems[i] = modelHeader.AddSubMenuItemCheckbox(lbl, "", checked)
		}
	}

	autostartChecked := t.actions.IsAutostartOn != nil && t.actions.IsAutostartOn()
	autostartItem := systray.AddMenuItemCheckbox("Запускать при логине", "Стартовать Murrly автоматически при входе в систему", autostartChecked)

	reloadItem := systray.AddMenuItem("Перезагрузить конфиг", "Перечитать config.toml")
	openCfgItem := systray.AddMenuItem("Открыть конфиг", "Открыть config.toml")

	// Permissions submenu — surfaces TCC privacy panes that the brief
	// AXIsProcessTrustedWithOptions toast otherwise hides before the
	// user can act. Skipped when no permission callbacks are wired
	// (Linux, where TCC doesn't apply).
	var micPermItem, axPermItem *systray.MenuItem
	if t.actions.OnOpenMicSettings != nil || t.actions.OnOpenAccessibility != nil {
		permHeader := systray.AddMenuItem("Разрешения", "Открыть системные настройки приватности")
		if t.actions.OnOpenMicSettings != nil {
			micPermItem = permHeader.AddSubMenuItem("Микрофон", "Privacy → Microphone")
		}
		if t.actions.OnOpenAccessibility != nil {
			axPermItem = permHeader.AddSubMenuItem("Accessibility", "Privacy → Accessibility")
		}
	}

	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Завершить Murrly", "Закрыть Murrly")

	// systray's API gives one ClickedCh per item; rather than blow up
	// the main select, fan submenu-item clicks out to per-item goroutines.
	for i, item := range modelItems {
		idx := i
		mi := item
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnPickModel != nil {
					t.actions.OnPickModel(idx)
				}
			}
		}()
	}
	if micPermItem != nil {
		mi := micPermItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnOpenMicSettings != nil {
					t.actions.OnOpenMicSettings()
				}
			}
		}()
	}
	if axPermItem != nil {
		mi := axPermItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnOpenAccessibility != nil {
					t.actions.OnOpenAccessibility()
				}
			}
		}()
	}

	go func() {
		for {
			select {
			case s := <-t.stateCh:
				if icon, ok := t.icons[s]; ok {
					systray.SetIcon(icon)
				}
				systray.SetTooltip("Murrly: " + stateName(s))
			case items := <-t.transcriptCh:
				updateTranscriptMenuItems(copyItems, items)
			case idx := <-t.activeModelCh:
				for i, item := range modelItems {
					if i == idx {
						item.Check()
					} else {
						item.Uncheck()
					}
				}
			case <-reloadItem.ClickedCh:
				if t.actions.OnReloadConfig != nil {
					t.actions.OnReloadConfig()
				}
			case <-openCfgItem.ClickedCh:
				if t.actions.OnOpenConfig != nil {
					t.actions.OnOpenConfig()
				}
			case <-copyLatestItem.ClickedCh:
				t.copyTranscript(0)
			case <-copyPreviousItem.ClickedCh:
				t.copyTranscript(1)
			case <-copyOlderItem.ClickedCh:
				t.copyTranscript(2)
			case <-reprocessItem.ClickedCh:
				if t.actions.OnReprocess != nil {
					t.actions.OnReprocess()
				}
			case <-autostartItem.ClickedCh:
				if t.actions.OnToggleAutostart != nil {
					if t.actions.OnToggleAutostart() {
						autostartItem.Check()
					} else {
						autostartItem.Uncheck()
					}
				}
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
	emptyLabels := []string{
		"— (последнее)",
		"— (предыдущее)",
		"— (ещё раньше)",
	}
	if text == "" {
		if index >= 0 && index < len(emptyLabels) {
			return emptyLabels[index]
		}
		return "— (пусто)"
	}
	// Show just the transcript fragment — clipboard semantics are obvious
	// from the menu's context.
	return transcriptPreview(text, 56)
}

func transcriptPreview(text string, limit int) string {
	compact := strings.Join(strings.Fields(text), " ")
	if limit <= 0 || utf8.RuneCountInString(compact) <= limit {
		return compact
	}
	runes := []rune(compact)
	return string(runes[:limit]) + "..."
}
