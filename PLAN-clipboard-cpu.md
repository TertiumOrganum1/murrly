# План переделки: буфер обмена, контекстная вставка, Whisper на CPU

Дата: 2026-09-16. Ветка: master (работаем прямо в ней, без PR).

## Мотивация (симптомы пользователя)

1. Системный буфер обмена периодически подвисает и тормозит; «gui за него цепляются».
2. Глюки остаются даже после выхода Murrly.
3. Вставка распознанного идёт не сразу — ~530 мс после распознавания.
4. Не всегда есть свободная видеопамять, нужен режим CPU.

## Результаты исследования (факты, установленные по коду и логам)

### Буфер обмена

- Конфиг пользователя: `insert_mode = "clipboard"`, `clipboard_replace = true`.
  Активен ТОЛЬКО буферный маршрут (`inserter.Clipboard.insertReplacing`).
- `internal/clipboard/clipboard_linux.go` запускает
  `xclip -selection clipboard -i -verbose` и **оставляет его владеть селекцией
  CLIPBOARD до следующей диктовки или до выхода**. То есть каждый Ctrl+V во
  всех программах весь сеанс обслуживается дочерним процессом Murrly.
- `-verbose` использовался для подсчёта fetch'ей (`WaitPasted`/`FetchTimes`).
  В replace-режиме эти методы **не вызывались ни разу** — а stderr-pipe, который
  ради них вычитывался, привязывал живучесть системного буфера к тому, успевает
  ли Murrly его разгребать.
  **ВАЖНО (выяснено при реализации): сам флаг `-verbose` убирать нельзя.**
  Без него xclip форкается в фон и родитель сразу выходит: процесс, который мы
  запустили, — не тот, что владеет селекцией, и `Kill()` не освобождает ничего
  (тест `TestPublishThenRelease` ловит это). С `-verbose` xclip остаётся на
  переднем плане и убивается. Оверхед снимается не удалением флага, а тем, что
  stderr уходит в /dev/null (`cmd.Stderr == nil`) и никто его не читает.
- `Release()` на выходе делает `cmd.Process.Kill()` — селекция остаётся без
  владельца. SIGQUIT намеренно не перехвачен (main.go), значит при крэше
  `Release` не отработает и осиротевший xclip продолжит владеть буфером.
- `confirmSelection` опрашивает селекцию через fork/exec `xclip -o` каждые
  20 мс до 1 с.
- Замеры из `~/.cache/murrly/murrly.log`: стабильные ~530 мс на вставку.
  Бюджет: Settle 50 + Save ≤300 + Set/confirm 30–80 + prePasteDelay 250 +
  pasteSettleDelay 150 + xdotool/xset ~30–50.
- В старых логах (preserving-режим, `murrly.log.1`):
  66 × «restored content did not stick after 2 attempts»,
  2 × «WE LOST THE SELECTION», 13 × «xclip TARGETS timed out» (по 2 с каждый).
- В системе живёт `csd-clipboard` (Cinnamon, X11) — он по XFixes подхватывает
  содержимое, когда владелец селекции умирает.

### Контекстная вставка (AT-SPI)

- `uicontext.Capture()` вызывается СИНХРОННО перед каждой вставкой
  (`cmd/murrly/uicontext_glue.go:adjustTextForContext`) с жёстким потолком
  **1500 мс** (`captureWithTimeout`).
- По логам ~половина вызовов возвращает `no-focused-element` / `opaque-editor`,
  то есть пользы не дала.
- `internal/a11ysetup/a11ysetup_vscode.go` прописывает в
  `~/.config/Code/User/settings.json` ключ
  `"editor.accessibilitySupport": "on"`. **Он сейчас там стоит.** Это
  постоянный режим скринридера в Monaco (скрытая DOM-копия текста,
  отключённая часть виртуализации) — стоячий тормоз, который живёт и когда
  Murrly выключена. Это и есть «глюки после выхода программы».
- Системный ключ `toolkit-accessibility` = **false** (проверено в обоих
  namespace: `org.gnome.desktop.interface` и `org.cinnamon.desktop.interface`).
  Значит общесистемного AT-SPI-налога на GTK/Qt нет — тормозит именно VS Code.
- Альтернатива уже есть и ничего не читает: **Ctrl+Shift+F12** →
  `adjustTextForcedMid` (`uicontext.Apply` с `ForceMid: true`) — понижает
  регистр первой буквы, добавляет пробел слева, снимает финальную точку.
  Чистая функция, работает всегда.

### Whisper CPU/GPU

- `whisper.device = "cuda"` в конфиге — **мёртвый ключ**, нигде не читается.
  Карта выбирается через `CUDA_VISIBLE_DEVICES` в `gpucheck.PreferDevice`.
- `output.restore_primary` — тоже мёртвый ключ (PRIMARY давно не трогается).
- Вендоренные Go-биндинги зашивают параметры намертво:
  `third_party/whisper.cpp/bindings/go/whisper.go:107`
  → `C.whisper_init_from_file_with_params(cPath, C.whisper_context_default_params())`.
  В `whisper_context_default_params()` поле `use_gpu = true`.
- `third_party/whisper.cpp` — НЕ submodule, а `git clone --depth 1` из
  `scripts/ensure-whisper-cpp.sh:6`, подключённый через `replace` в go.mod.
  Правка прямо в нём затрётся при следующем клоне.
- `mk/linux.mk:74-75,89-90` выставляют `C_INCLUDE_PATH` / `LIBRARY_PATH` на
  всю команду `go build` / `go test`.
- `gpucheck` уже умеет всё для решения о памяти: `ParseDevices` (парсит
  `nvidia-smi --query-gpu=uuid,name,memory.free,memory.total`),
  `Decide(freeMiB, modelBytes)` → `VerdictOK` / `VerdictTight` /
  `VerdictImpossible`, `SelectWeakest`.
- Модели на диске: `~/.local/share/murrly/models/` — `ggml-large-v3-turbo-q5_0.bin`
  (547 МБ, квантованная), `ggml-large-v3-turbo.bin` (1549 МБ),
  `ggml-large-v3.bin` (2952 МБ). CPU: 32 потока.

## Ограничение X11, которое обойти нельзя

В X11 нет хранилища буфера обмена. Буфер — это процесс: владелец селекции
держит данные у себя и отдаёт их по запросу. Чтобы Ctrl+V вообще что-то
вставил, в момент нажатия текст обязан лежать в селекции, то есть кто-то
обязан ею владеть. Вопрос не «владеть или нет», а **как долго**.

Решение: владеем ~200 мс вокруг вставки, потом гасим свой xclip и уходим из
цепочки Ctrl+V. Дальше владение подхватывает `csd-clipboard` (или буфер
пустеет — приемлемо, прежнее содержимое лежит у нас в памяти и достаётся из
меню).

## Принятые решения

- **Только plain text.** Rich text отпадает: `xclip` умеет публиковать ровно
  ОДИН target на процесс, и публикация через `-t text/html` воспроизводит
  ровно тот баг, который раньше вешал десктоп (селекция без текстового
  таргета; см. комментарии в `pickBinaryTarget` / `isTextTarget`). Картинки
  игнорируем — согласованный компромисс.
- **Снимок прежнего буфера — в память Go**, не в X. Одно разовое чтение
  `xclip -o` на нажатии F12, параллельно записи, best-effort. Не получилось —
  молча забыли, никто не ждёт.
- **Ни одного ожидания события.** Никаких `WaitPasted`, подтверждений
  владения, адаптивных задержек. Остаётся один `pasteSettleDelay` 150 мс
  (защита от литеральной «v», когда синтетический Ctrl догоняет ещё
  отпускающийся F12) и один `holdAfterPaste` 200 мс перед тем как погасить
  владельца.
- **Whisper CPU/GPU: patch-файл в нашем репозитории**, применяемый
  `ensure-whisper-cpp.sh`.
  ВАЖНО, поправка к первоначальной идее «тонкий cgo-файл у нас»: так нельзя.
  Типы cgo привязаны к пакету — `*C.struct_whisper_context`, созданный в
  нашем пакете, невозможно передать в API биндингов, это разные Go-типы.
  Поэтому меняем сам биндинг, но патч лежит у нас и переживает re-clone.
- `use_gpu` — параметр **загрузки модели** (`whisper_init_from_file_with_params`),
  не параметр инференса. Значит переключение CPU↔GPU = перезагрузка модели
  (механика уже есть: `releaseModel` / `switchModel`), а не перезапуск процесса.

---

# Шаг 1. Буфер обмена

## 1.1 `internal/clipboard`

Новая роль пакета: тонкая обёртка над системным буфером, которая ничего не
держит дольше вставки.

**`clipboard.go` (кроссплатформенный):**
- `Saved` упростить до `{ Text string; HasContent bool }`. Убрать
  `Primary`, `HasPrimary`, `Binary`, `Target`, `platformState`.
- `Stash` (Put/Peek) оставить как есть — он уже то, что нужно.
- `Clipboard` — убрать `RestorePrimary` и встроенный `pasteTracker`.

**`clipboard_linux.go` — новый состав:**
- `ReadText() (Saved, bool)` — один `xclip -selection clipboard -o` под
  коротким таймаутом (`readTimeout = 400ms`). Никакого чтения TARGETS,
  никакого бинарного пути.
- `Publish(text string) (release func(), err error)` — `exec.Command("xclip",
  "-selection", "clipboard", "-verbose", "-i")` со `Stderr == nil`
  (в /dev/null — читать нечего, но без флага процесс уйдёт в фон и станет
  неубиваемым), `Start()`, вернуть
  замыкание, которое убивает процесс и реапает его. Никакого
  `confirmSelection`.
- `Set(text string) error` — остаётся для меню/paste-last: `Publish` +
  сохранение владельца до следующего `Set` (здесь владение осмысленно: юзер
  явно просил положить текст в буфер).

**Удалить целиком:** `SaveWithin`, `Save`, `Restore`, `WaitPasted`,
`ArmPasteWait`, `FetchTimes`, `ServesText`, `confirmSelection`,
`writeSelectionTracked`, `xclipOwner`, `pasteTracker`, `restoredOK`,
`restoreAttempts`, `restoreSettleDelay`, `pickBinaryTarget`,
`writeSelectionBinary`, `clearSelection`, `parseRequestNumber`,
`requestNumberRe`, `readTargets`, `parseTargets`, `isTextTarget`,
`containsTarget`, `clipboardClaimTimeout`.
Соответственно почистить `clipboard_test.go`.

**`clipboard_darwin.go` / `clipboard_windows.go`:** там настоящее хранилище,
владения нет. `Publish` = `Set` + no-op release. `ReadText` = чтение текста.
Убрать `Save`/`Restore`, если станут не нужны.

## 1.2 `internal/inserter/clipboard.go`

Сводится к:

```go
type ClipboardBackend interface {
    Publish(text string) (release func(), err error)
}

const holdAfterPaste = 200 * time.Millisecond

func (c *Clipboard) Insert(text string) error {
    if text == "" { return nil }
    release, err := c.CB.Publish(text)
    if err != nil { return fmt.Errorf("clipboard.Publish: %w", err) }
    defer release()
    if err := c.Paster.Paste(func() {}); err != nil {
        return fmt.Errorf("paster.Paste: %w", err)
    }
    time.Sleep(holdAfterPaste)
    return nil
}
```

**Удалить:** поле `Replace`, `insertReplacing`, `OnDisplaced`,
`stashDisplaced`, `snapshotBackend`, `displacedSnapshotWait`,
`FetchReporter`, `ClipboardConfirmer`, `consumerReadWait`, `lateReadWait`,
`postReadHold`, `prePasteDelay`, `postPasteDelay`, `adaptiveWait`,
`goodPastes`, `maxPrePaste`, `prePasteStep`, `prePasteRelief`, `reliefAfter`,
`currentPrePaste`, `notePasteOutcome`, использование `activeWindowID`.
Поле `PasteDelay` больше ни на что не влияет — убрать из структуры и из
`ForMode`.

`inserter.go`: `ForMode` теряет параметры `pasteDelay` и `replaceClipboard`.
`Chain.OnClipboardDisplaced` удалить (снимок переезжает в app).

Файлы `window_linux.go` / `window_other.go` (`activeWindowID`) удалить, если
других потребителей нет.

## 1.3 Снимок переезжает на нажатие F12

- `internal/app/app.go`: в `Config` добавить `OnRecordStart func()`.
  Вызывать в `handle()` в ветках `EventKeyDown` и `EventKeyDownForceMid`
  сразу после успешного `Recorder.Start()`.
  Интерфейс `app.Clipboard` (Save/Set/Restore) удалить — он больше не нужен,
  как и поле `Config.Clipboard`, и дефолтная сборка `inserter.Clipboard` в
  `New()` (там же убрать дефолт `PasteDelay`).
- `cmd/murrly/main.go`: `appCfg.OnRecordStart = func() { go snapshotClipboard() }`,
  где `snapshotClipboard` делает `cb.ReadText()` и при успехе
  `displaced.Put(s)` + `t.SetDisplacedClipboard(true)`.

## 1.4 Конфиг и меню

- `config.go`: ключи `clipboard_replace`, `restore_primary`, `paste_delay_ms`
  оставить парсящимися (чтобы старые конфиги не падали), но пометить
  deprecated и нигде не использовать.
- `cmd/murrly/main.go`: удалить `persistClipboardReplace`,
  `actions.IsClipboardReplace`, `actions.OnToggleClipboardReplace`,
  `cb.Release()` в конце `main` (владельцев больше не копим), `clipAdapter`
  если станет не нужен.
- `internal/menuactions/actions.go`: удалить `IsClipboardReplace` /
  `OnToggleClipboardReplace`.
- `internal/tray/tray.go`: удалить пункт «Затирать буфер обмена».
  Пункт «Вернуть прежний буфер обмена» ОСТАВИТЬ — он теперь публикует текст
  из нашей памяти через `cb.Set`.
- `main.go: OnRestoreDisplacedClipboard` → `cb.Set(s.Text)` вместо
  `cb.Restore(s)`.

## 1.5 Ожидаемый результат

Бюджет вставки: ~530 мс → ~200 мс (Settle 50 + pasteSettleDelay 150 + xdotool).
Murrly не владеет селекцией нигде, кроме ~200 мс вокруг вставки и пунктов
меню, где пользователь сам просил положить текст в буфер.

---

# Шаг 2. Контекстная вставка — выключить

- `internal/config/config.go`: дефолт `ContextInsert: false`.
- `internal/a11ysetup/a11ysetup_vscode.go`: перестать выставлять
  `"editor.accessibilitySupport": "on"`. Функцию переделать в обратную —
  ставит `"off"`, и вызвать её из `Apply()`; либо убрать патч VS Code из
  `Apply()` совсем и оставить только gsettings-часть.
- Живой файл пользователя `~/.config/Code/User/settings.json`: вернуть
  `"editor.accessibilitySupport": "off"`.
- Живой конфиг `~/.config/murrly/config.toml`: `context_insert = false`.
- Ctrl+Shift+F12 (`adjustTextForcedMid`) не трогать — это замена.

---

# Шаг 3. Whisper на CPU

## 3.1 Патч биндингов

- Создать `patches/whisper-go-use-gpu.patch`.
  В `third_party/whisper.cpp/bindings/go/whisper.go` добавить рядом с
  существующим `Whisper_init`:
  ```go
  func Whisper_init_with_gpu(path string, useGPU bool) *Context {
      cPath := C.CString(path)
      defer C.free(unsafe.Pointer(cPath))
      params := C.whisper_context_default_params()
      params.use_gpu = C.bool(useGPU)
      if ctx := C.whisper_init_from_file_with_params(cPath, params); ctx != nil {
          return (*Context)(ctx)
      }
      return nil
  }
  ```
  В `pkg/whisper/model.go` добавить `NewWithGPU(path string, useGPU bool)`,
  повторяющую `New`, но вызывающую `Whisper_init_with_gpu`.
- `scripts/ensure-whisper-cpp.sh`: после клона применять патч
  (`git -C third_party/whisper.cpp apply ../../patches/whisper-go-use-gpu.patch`),
  идемпотентно (`git apply --check` перед применением).

## 3.2 Наша сторона

- `internal/config/config.go`: оживить `Whisper.Device`. Допустимые значения:
  `"auto"` (дефолт), `"cuda"`, `"cpu"`.
- `internal/transcriber`: пробросить `UseGPU bool` в загрузчик модели,
  вызывать `whisper.NewWithGPU(path, useGPU)`.
- `cmd/murrly/main.go` / `loader.go`:
  - На старте при `device = "auto"`: `gpucheck.ParseDevices` +
    `gpucheck.Decide(freeMiB, modelBytes)`. `VerdictImpossible` → стартуем на
    CPU вместо аварийного выхода, с записью в лог и desktop-уведомлением.
  - При `device = "cpu"` (или автопадении на CPU) принудительно:
    квантованная модель `ggml-large-v3-turbo-q5_0.bin` и
    `multi_inference_count = 1` (иначе время ×3).
  - Рантайм-переключение: пункт трея «Распознавание: GPU / CPU» →
    `releaseModel()` + перезагрузка модели с новым `useGPU`
    (по той же механике, что `switchModel`), + `persistWhisperDevice`.
- `internal/menuactions` + `internal/tray`: пункт переключения и его
  состояние.

---

# Порядок работ

1. Шаг 1 (буфер) — самый крупный, даёт основной выигрыш.
2. Шаг 2 (контекстная вставка) — мелкий, независимый.
3. `make build` / `make test`.
4. Шаг 3 (CPU) — отдельным заходом после того, как 1–2 проверены руками.

# Как тестировать (для пользователя, после сборки)

- Диктовка F12 в разных окнах: текст должен появляться заметно быстрее.
- Сразу после диктовки `ps aux | grep xclip` — **не должно быть ни одного**
  живого xclip от murrly.
- Скопировать что-нибудь, продиктовать, затем меню трея → «Вернуть прежний
  буфер обмена» → Ctrl+V отдаёт прежний текст.
- Ctrl+V в любой программе во время работы murrly — без задержек.
- VS Code: убедиться, что `editor.accessibilitySupport` = `"off"`.
