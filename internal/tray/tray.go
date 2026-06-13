// Package tray displays the systray icon and exposes the user actions.
package tray

import (
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/systray"

	"github.com/tertiumorganum1/murrly/internal/menuactions"
	"github.com/tertiumorganum1/murrly/internal/ruprofane"
)

type State int

const (
	StateIdle State = iota
	StateRecording
	StateTranscribing
	StateError
)

type Tray struct {
	icons           map[State][]byte
	stateCh         chan State
	transcriptCh    chan []string
	activeModelCh   chan int
	activeScoringCh chan int
	nemoStatusCh    chan string
	actions         *menuactions.Actions
}

func New(icons map[State][]byte, actions *menuactions.Actions) *Tray {
	return &Tray{
		icons:           icons,
		stateCh:         make(chan State, 8),
		transcriptCh:    make(chan []string, 8),
		activeModelCh:   make(chan int, 8),
		activeScoringCh: make(chan int, 8),
		nemoStatusCh:    make(chan string, 8),
		actions:         actions,
	}
}

// SetNemotronStatus updates the disabled "Nemotron: …" status line. No-op
// if the Nemotron menu group isn't shown (callback nil).
func (t *Tray) SetNemotronStatus(s string) {
	select {
	case t.nemoStatusCh <- s:
	default:
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

// SetActiveScoring moves the checkmark in the scoring-mode group to the
// item at the given index, clearing the others. Pass -1 to clear all.
// Driven from the scoring-pick callback so a pick from either the tray or
// the Dock menu keeps both in sync.
func (t *Tray) SetActiveScoring(index int) {
	select {
	case t.activeScoringCh <- index:
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
	// the engines with a small silence prefix. Cheap manual retry when
	// the first decode dropped punctuation or otherwise looks bad.
	// The hotkey lives in the title — the menu doubles as the help.
	reprocessItem := systray.AddMenuItem("Распознать ещё раз (Ctrl+F12)", "Прогнать последнюю запись через движки ещё раз (со сдвигом окна); Ctrl+Break — вставить вариант Nemotron")

	// Menu twin of the Ctrl+F11 picker hotkey, doubling as its help.
	// Hidden when no picker is wired (single-pass mode).
	var variantsItem *systray.MenuItem
	if t.actions.OnPickVariants != nil {
		variantsItem = systray.AddMenuItem("Варианты распознавания (Ctrl+F11)", "Показать варианты последнего распознавания и вставить выбранный")
	}

	// Model picker. Cinnamon/AppIndicator renders NESTED submenus
	// unreliably — they collapse to an empty little square (the bug the
	// user hit). So the model choices live as FLAT top-level checkable
	// items ("Модель: <name>") instead of under a "Модель ▸" submenu;
	// top-level checkboxes render fine (same as the autostart toggle).
	// Hidden entirely when fewer than 2 models are present.
	var modelItems []*systray.MenuItem
	if len(t.actions.ModelLabels) >= 2 {
		systray.AddSeparator()
		modelItems = make([]*systray.MenuItem, len(t.actions.ModelLabels))
		for i, lbl := range t.actions.ModelLabels {
			checked := i == t.actions.ActiveModelIndex
			modelItems[i] = systray.AddMenuItemCheckbox("Модель: "+lbl, "Переключить модель Whisper", checked)
		}
	}

	// Multi-inference on/off (only when the engine is built). Off → F12 is
	// a single pass on the original sample, no variant batch and no
	// Ctrl+F11 picker; Ctrl+F12 reprocess behaves like the old single-pass
	// retry. Placed above the scoring group it gates.
	var multiItem *systray.MenuItem
	if t.actions.OnToggleMulti != nil {
		systray.AddSeparator()
		multiChecked := t.actions.IsMultiOn != nil && t.actions.IsMultiOn()
		multiItem = systray.AddMenuItemCheckbox("Множественное распознавание", "Распознавать несколько вариантов и выбирать лучший (Ctrl+F11 — выбрать вручную)", multiChecked)
	}

	// Scoring-mode picker (multi-inference only). Flat checkable items
	// like the model picker — same Cinnamon nested-submenu bug applies.
	// Omitted when no callback is wired (single-pass / non-Linux).
	var scoringItems []*systray.MenuItem
	if t.actions.OnPickScoringMode != nil && len(t.actions.ScoringLabels) >= 2 {
		systray.AddSeparator()
		scoringItems = make([]*systray.MenuItem, len(t.actions.ScoringLabels))
		for i, lbl := range t.actions.ScoringLabels {
			checked := i == t.actions.ActiveScoringIndex
			scoringItems[i] = systray.AddMenuItemCheckbox("Оценка: "+lbl, "Как выбирается лучший вариант распознавания", checked)
		}
	}

	autostartChecked := t.actions.IsAutostartOn != nil && t.actions.IsAutostartOn()
	autostartItem := systray.AddMenuItemCheckbox("Запускать при логине", "Стартовать Murrly автоматически при входе в систему", autostartChecked)

	padSilenceChecked := t.actions.IsPadSilenceOn != nil && t.actions.IsPadSilenceOn()
	padSilenceItem := systray.AddMenuItemCheckbox("Тишина по краям", "Добавлять 1 с тишины с обеих сторон каждой записи перед Whisper", padSilenceChecked)

	profanityChecked := t.actions.IsProfanityOn != nil && t.actions.IsProfanityOn()
	profanityItem := systray.AddMenuItemCheckbox("Фильтр лексики", "Маскировать обсценную лексику символом «•» при показе и вставке; оригинал хранится без цензуры", profanityChecked)

	profanityRemoveChecked := t.actions.IsProfanityRemove != nil && t.actions.IsProfanityRemove()
	profanityRemoveItem := systray.AddMenuItemCheckbox("Вырезать, а не маскировать", "Когда «Фильтр лексики» включён — вырезать обсценные слова целиком (с прилегающей пунктуацией), а не закрывать «•». Обратимо: оригинал хранится без цензуры", profanityRemoveChecked)
	// The cut-out option only applies while the filter is on — grey it out
	// otherwise.
	if !profanityChecked {
		profanityRemoveItem.Disable()
	}

	// Nemotron enable/disable (Linux). Off by default — loads a multi-GB GPU
	// model — so this is a checkbox the user opts into; turning it on starts
	// the sidecar but the engine wires only on the next Murrly start.
	var nemoToggleItem *systray.MenuItem
	if t.actions.OnToggleNemotron != nil {
		nemoOn := t.actions.IsNemotronOn != nil && t.actions.IsNemotronOn()
		nemoToggleItem = systray.AddMenuItemCheckbox("Движок Nemotron (Break)", "Второй ASR-движок на клавише Break; грузит модель в GPU. Вступает в силу после перезапуска Murrly", nemoOn)
	}

	// Context-insert prerequisites (Linux): one self-describing item.
	// Not yet set up → an actionable "включить…" button; everything in
	// place → a disabled "настроена ✓" status line. Clicking applies
	// the system + VS Code settings via the wired callback.
	var ctxInsertItem *systray.MenuItem
	if t.actions.OnSetupContextInsert != nil {
		ready := t.actions.IsContextInsertReady != nil && t.actions.IsContextInsertReady()
		ctxInsertItem = systray.AddMenuItem(contextInsertTitle(ready), "Читать текст вокруг курсора при вставке и подгонять регистр, пробелы и точку")
		if ready {
			ctxInsertItem.Disable()
		}
	}

	// Nemotron group (Linux only — shown when the restart callback is wired).
	// A disabled status line shows the ~48 s model load; a restart item
	// recovers a wedged sidecar.
	var nemoStatusItem, nemoRestartItem *systray.MenuItem
	if t.actions.OnRestartNemotron != nil {
		systray.AddSeparator()
		nemoStatusItem = systray.AddMenuItem("Nemotron: загружается…", "Состояние сайдкара Nemotron")
		nemoStatusItem.Disable()
		nemoRestartItem = systray.AddMenuItem("Перезапустить Nemotron", "Перезапустить сайдкар-сервис Nemotron")
	}

	reloadItem := systray.AddMenuItem("Перезагрузить конфиг", "Перечитать config.toml")
	openCfgItem := systray.AddMenuItem("Открыть конфиг", "Открыть config.toml")
	openLogItem := systray.AddMenuItem("Открыть лог", "Открыть файл лога Murrly")

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
	// Multi-inference toggle — flips the live state and re-ticks itself
	// from the returned value. Conditional item, so it runs in its own
	// goroutine rather than the main select.
	if multiItem != nil {
		mi := multiItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnToggleMulti != nil {
					if t.actions.OnToggleMulti() {
						mi.Check()
					} else {
						mi.Uncheck()
					}
				}
			}
		}()
	}

	// Scoring-mode items behave as a radio group: clicking one fires the
	// callback, which re-ticks the group through activeScoringCh (so a pick
	// from the Dock menu moves this group's checkmark too, and vice versa).
	for i, item := range scoringItems {
		idx := i
		mi := item
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnPickScoringMode != nil {
					t.actions.OnPickScoringMode(idx)
				}
			}
		}()
	}
	// "Варианты распознавания" — fires the same event path as the
	// Ctrl+F11 hotkey. Conditional item → its own goroutine.
	if variantsItem != nil {
		mi := variantsItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnPickVariants != nil {
					t.actions.OnPickVariants()
				}
			}
		}()
	}
	// Context-insert setup — applies the missing settings and, once
	// everything is in place, turns itself into a disabled status line.
	if ctxInsertItem != nil {
		mi := ctxInsertItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnSetupContextInsert == nil {
					continue
				}
				if t.actions.OnSetupContextInsert() {
					mi.SetTitle(contextInsertTitle(true))
					mi.Disable()
				}
			}
		}()
	}
	// Nemotron enable/disable toggle — flips state + (dis)starts the sidecar
	// via the callback, re-ticking from the returned value.
	if nemoToggleItem != nil {
		mi := nemoToggleItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnToggleNemotron != nil {
					if t.actions.OnToggleNemotron() {
						mi.Check()
					} else {
						mi.Uncheck()
					}
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

	// Nemotron restart click + status poller (conditional items run outside
	// the main select, like the multi-inference toggle above).
	if nemoRestartItem != nil {
		mi := nemoRestartItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnRestartNemotron != nil {
					t.actions.OnRestartNemotron()
					t.SetNemotronStatus("перезапуск…")
				}
			}
		}()
	}
	if nemoStatusItem != nil && t.actions.NemotronStatus != nil {
		go func() {
			for {
				t.SetNemotronStatus(t.actions.NemotronStatus())
				time.Sleep(3 * time.Second)
			}
		}()
	}

	// Cache the latest recent-phrase list so toggling the profanity filter
	// can re-render those menu titles immediately under the new state.
	var lastTranscripts []string
	go func() {
		for {
			select {
			case s := <-t.stateCh:
				if icon, ok := t.icons[s]; ok {
					systray.SetIcon(icon)
				}
				systray.SetTooltip("Murrly: " + stateName(s))
			case items := <-t.transcriptCh:
				lastTranscripts = items
				updateTranscriptMenuItems(copyItems, items)
			case idx := <-t.activeModelCh:
				for i, item := range modelItems {
					if i == idx {
						item.Check()
					} else {
						item.Uncheck()
					}
				}
			case idx := <-t.activeScoringCh:
				for i, item := range scoringItems {
					if i == idx {
						item.Check()
					} else {
						item.Uncheck()
					}
				}
			case s := <-t.nemoStatusCh:
				if nemoStatusItem != nil {
					nemoStatusItem.SetTitle("Nemotron: " + s)
				}
			case <-reloadItem.ClickedCh:
				if t.actions.OnReloadConfig != nil {
					t.actions.OnReloadConfig()
				}
			case <-openCfgItem.ClickedCh:
				if t.actions.OnOpenConfig != nil {
					t.actions.OnOpenConfig()
				}
			case <-openLogItem.ClickedCh:
				if t.actions.OnOpenLog != nil {
					t.actions.OnOpenLog()
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
			case <-padSilenceItem.ClickedCh:
				if t.actions.OnTogglePadSilence != nil {
					if t.actions.OnTogglePadSilence() {
						padSilenceItem.Check()
					} else {
						padSilenceItem.Uncheck()
					}
				}
			case <-profanityItem.ClickedCh:
				if t.actions.OnToggleProfanity != nil {
					if t.actions.OnToggleProfanity() {
						profanityItem.Check()
						profanityRemoveItem.Enable()
					} else {
						profanityItem.Uncheck()
						profanityRemoveItem.Disable()
					}
					// Re-censor (or restore) the recent-phrase titles at once.
					updateTranscriptMenuItems(copyItems, lastTranscripts)
				}
			case <-profanityRemoveItem.ClickedCh:
				if t.actions.OnToggleProfanityRemove != nil {
					if t.actions.OnToggleProfanityRemove() {
						profanityRemoveItem.Check()
					} else {
						profanityRemoveItem.Uncheck()
					}
					updateTranscriptMenuItems(copyItems, lastTranscripts)
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

// contextInsertTitle renders the context-insert item for the two
// states it can be in: an actionable setup button or a done-marker.
func contextInsertTitle(ready bool) string {
	if ready {
		return "Контекстная вставка: настроена ✓"
	}
	return "Контекстная вставка: включить…"
}

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
		// Censor only the displayed title (toggle-gated, no-op when off);
		// enable/disable still keys off the real (uncensored) emptiness.
		item.SetTitle(transcriptMenuTitle(i, ruprofane.Filter(text)))
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
