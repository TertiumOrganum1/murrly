// Package tray displays the systray icon and exposes the user actions.
package tray

import (
	"strings"
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
	displacedCh     chan bool
	activeModelCh   chan int
	activeScoringCh chan int
	actions         *menuactions.Actions
}

func New(icons map[State][]byte, actions *menuactions.Actions) *Tray {
	return &Tray{
		icons:           icons,
		stateCh:         make(chan State, 8),
		transcriptCh:    make(chan []string, 8),
		displacedCh:     make(chan bool, 8),
		activeModelCh:   make(chan int, 8),
		activeScoringCh: make(chan int, 8),
		actions:         actions,
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

// SetDisplacedClipboard shows or hides the "previous clipboard" item. Called
// once the replacing insert has actually displaced something, so a session
// that never overwrote a clipboard never grows a row that would do nothing.
func (t *Tray) SetDisplacedClipboard(has bool) {
	select {
	case t.displacedCh <- has:
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
	// menu is to recopy a recent recognition, so it lives at the top. The
	// count comes from config (output.recent_transcripts, default 20). Empty
	// slots are hidden so a fresh start isn't a wall of "—" rows; each fills
	// and shows as recognitions arrive (transcriptSlots.render).
	recentCount := t.actions.RecentCount
	if recentCount <= 0 {
		recentCount = 3
	}
	copyItems := make([]*systray.MenuItem, recentCount)
	for i := range copyItems {
		it := systray.AddMenuItem("—", "Скопировать в буфер обмена")
		it.Hide()
		copyItems[i] = it
		idx := i
		go func() {
			for range it.ClickedCh {
				t.copyTranscript(idx)
			}
		}()
	}

	slots := newTranscriptSlots(copyItems)

	// Directly under the phrase slots, and for the same reason: this is the
	// other thing the menu hands back to the clipboard. It only ever appears
	// in the replacing mode, where the dictation overwrites whatever was
	// there — the content is snapshotted just before that happens and this
	// puts it back. Hidden until there is a snapshot to offer.
	displacedItem := systray.AddMenuItem("Вернуть прежний буфер обмена", "Положить обратно то, что лежало в буфере обмена до последней вставки, затершей его (текст или картинку)")
	displacedItem.Hide()
	go func() {
		for range displacedItem.ClickedCh {
			if t.actions.OnRestoreDisplacedClipboard != nil {
				t.actions.OnRestoreDisplacedClipboard()
			}
		}
	}()

	// "Reprocess last" — re-runs the most recent recording through
	// the engines with a small silence prefix. Cheap manual retry when
	// the first decode dropped punctuation or otherwise looks bad.
	// The hotkey lives in the title — the menu doubles as the help.
	reprocessItem := systray.AddMenuItem("Распознать ещё раз (Ctrl+F12)", "Прогнать последнюю запись через движок ещё раз (со сдвигом окна)")

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

	directChecked := t.actions.IsDirectInsert != nil && t.actions.IsDirectInsert()
	directItem := systray.AddMenuItemCheckbox("Прямой ввод", "Вставлять текст прямо в поле (через шину доступности, иначе набором на клавиатуре), не трогая буфер обмена. Выключено — прежний способ: подменить буфер, нажать Ctrl+V, вернуть обратно. Прямой ввод не может потерять буфер, но длинную фразу набирает несколько секунд", directChecked)

	clipReplaceChecked := t.actions.IsClipboardReplace != nil && t.actions.IsClipboardReplace()
	clipReplaceItem := systray.AddMenuItemCheckbox("Затирать буфер обмена", "Только для вставки через буфер обмена. Выключено — прежний способ: запомнить буфер, вставить, вернуть исходное. Включено — распознанная фраза просто остаётся в буфере: ничего не ждём и не возвращаем, поэтому Chromium/Electron не подсовывают прошлое содержимое", clipReplaceChecked)
	// Meaningless while the text goes straight into the field.
	if directChecked {
		clipReplaceItem.Disable()
	}

	profanityRemoveChecked := t.actions.IsProfanityRemove != nil && t.actions.IsProfanityRemove()
	profanityRemoveItem := systray.AddMenuItemCheckbox("Вырезать, а не маскировать", "Когда «Фильтр лексики» включён — вырезать обсценные слова целиком (с прилегающей пунктуацией), а не закрывать «•». Обратимо: оригинал хранится без цензуры", profanityRemoveChecked)
	// The cut-out option only applies while the filter is on — grey it out
	// otherwise.
	if !profanityChecked {
		profanityRemoveItem.Disable()
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

	// eXpress relaunch-with-accessibility (Linux, shown only when eXpress is
	// installed). eXpress rewrites its own autostart from in-app settings, so a
	// button that relaunches it with --force-renderer-accessibility is the
	// reliable way to make its message field readable for context-insert.
	var expressItem *systray.MenuItem
	if t.actions.OnRestartExpress != nil {
		expressItem = systray.AddMenuItem("Перезапустить eXpress (доступность)", "Перезапустить eXpress с флагом доступности, чтобы вставка читала его поле")
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
	// eXpress relaunch — fire the callback (kills + relaunches eXpress with the
	// accessibility flag). Stays clickable so the user can re-run it after an
	// eXpress restart re-spawned a bare instance.
	if expressItem != nil {
		mi := expressItem
		go func() {
			for range mi.ClickedCh {
				if t.actions.OnRestartExpress != nil {
					t.actions.OnRestartExpress()
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
				slots.render(items)
			case has := <-t.displacedCh:
				if has {
					displacedItem.Show()
				} else {
					displacedItem.Hide()
				}
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
			case <-directItem.ClickedCh:
				if t.actions.OnToggleDirectInsert != nil {
					if t.actions.OnToggleDirectInsert() {
						directItem.Check()
						clipReplaceItem.Disable()
					} else {
						directItem.Uncheck()
						clipReplaceItem.Enable()
					}
				}
			case <-clipReplaceItem.ClickedCh:
				if t.actions.OnToggleClipboardReplace != nil {
					if t.actions.OnToggleClipboardReplace() {
						clipReplaceItem.Check()
					} else {
						clipReplaceItem.Uncheck()
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
					slots.render(lastTranscripts)
				}
			case <-profanityRemoveItem.ClickedCh:
				if t.actions.OnToggleProfanityRemove != nil {
					if t.actions.OnToggleProfanityRemove() {
						profanityRemoveItem.Check()
					} else {
						profanityRemoveItem.Uncheck()
					}
					slots.render(lastTranscripts)
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

// transcriptSlots owns the recent-phrase rows and remembers what each one is
// currently showing.
//
// The memory is the point. Every write to a row — a title, a show, a hide —
// is a D-Bus property update to the desktop's tray applet, and any of them
// makes it rebuild the whole menu. Re-rendering all twenty rows on every
// dictation meant sixty updates and a full rebuild each time a single new
// phrase arrived at the top. Cinnamon's xapp-sn-watcher does not survive that
// indefinitely: it starts destroying its own menu windows and then fails an
// assertion on every attempt to show one, which reads to the user as the
// desktop freezing for seconds at a time.
//
// So: compare against what is already on screen and write only the rows that
// actually differ. A new phrase touches one row and shifts the rest down;
// nothing else moves.
type transcriptSlots struct {
	items []menuRow
	// shown[i] is the title row i is displaying, "" while it is hidden.
	shown []string
}

// menuRow is the part of *systray.MenuItem these rows use — an interface only
// so a test can count how many writes a render actually makes, which is the
// entire point of the type.
type menuRow interface {
	SetTitle(string)
	Enable()
	Show()
	Hide()
}

func newTranscriptSlots(items []*systray.MenuItem) *transcriptSlots {
	rows := make([]menuRow, len(items))
	for i, it := range items {
		rows[i] = it
	}
	// The rows are created hidden, so every slot starts out showing nothing.
	return &transcriptSlots{items: rows, shown: make([]string, len(items))}
}

func (s *transcriptSlots) render(transcripts []string) {
	for i, item := range s.items {
		want := ""
		if i < len(transcripts) && transcripts[i] != "" {
			// Censor only the displayed title (toggle-gated, no-op when off);
			// the stored phrase stays uncensored.
			want = transcriptPreview(ruprofane.Filter(transcripts[i]), 56)
		}
		if want == s.shown[i] {
			continue
		}
		switch {
		case want == "":
			// Empty slot — hide it so the menu shows only real phrases.
			item.Hide()
		case s.shown[i] == "":
			item.SetTitle(want)
			item.Enable()
			item.Show()
		default:
			item.SetTitle(want)
		}
		s.shown[i] = want
	}
}

func transcriptPreview(text string, limit int) string {
	compact := strings.Join(strings.Fields(text), " ")
	if limit <= 0 || utf8.RuneCountInString(compact) <= limit {
		return compact
	}
	runes := []rune(compact)
	return string(runes[:limit]) + "..."
}
