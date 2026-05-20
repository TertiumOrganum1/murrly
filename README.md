# voice-input

`voice-input` — локальный push-to-talk ввод текста голосом для Linux.
Удерживаете `F12`, говорите, отпускаете клавишу — распознанный текст
вставляется в активное окно через буфер обмена.

Распознавание работает локально через `whisper.cpp`. Аудио не отправляется во
внешние сервисы.

## Возможности

- глобальная клавиша push-to-talk;
- распознавание речи через локальную модель Whisper;
- вставка результата в текущее окно через `xclip` и `xdotool`;
- иконка и меню в системном трее;
- постобработка текста: пробелы после пунктуации, удаление типичных
  hallucination-фраз Whisper вроде `Продолжение следует...`,
  `Субтитры сделал ...`, `В этом видео я покажу ...`;
- конфиг в TOML.

## Ограничения

- рассчитано на Linux/X11;
- для Wayland может не работать глобальный hotkey, `xclip` или `xdotool`;
- сборка по умолчанию использует CUDA и NVIDIA GPU;
- модель Whisper не хранится в репозитории и скачивается отдельно.

## Быстрый старт на Debian/Ubuntu

```bash
git clone https://github.com/tertiumorganum1/voice-input.git
cd voice-input
scripts/bootstrap-ubuntu.sh
```

Скрипт:

- ставит системные зависимости через `sudo apt-get`;
- собирает `whisper.cpp`;
- скачивает модель `ggml-large-v3.bin`;
- копирует модель в `~/.local/share/voice-input/models/`;
- собирает бинарник `bin/voice-input`;
- устанавливает приложение в `~/.local/bin/voice-input`;
- добавляет ярлык в меню приложений.

Если CUDA toolkit не установлен, можно разрешить скрипту поставить пакет из
репозитория дистрибутива:

```bash
INSTALL_CUDA_TOOLKIT=1 scripts/bootstrap-ubuntu.sh
```

Чтобы включить автозапуск при входе в графическую сессию:

```bash
AUTOSTART=1 scripts/bootstrap-ubuntu.sh
```

Можно выбрать другую модель:

```bash
MODEL=large-v3-turbo scripts/bootstrap-ubuntu.sh
```

## Ручная сборка

```bash
make whisper
make model
make build
```

Установка бинарника, ярлыка приложения и локально скачанных моделей:

```bash
make install
```

Если была установлена старая версия как user-service, `make install` отключит
ее и уберет старый unit-файл.

После установки запустите `voice-input` из меню приложений или командой:

```bash
~/.local/bin/voice-input
```

Приложение работает как обычный tray-app. В трее есть меню, через которое его
можно закрыть.

Автозапуск:

```bash
make autostart
```

Отключить автозапуск:

```bash
make uninstall-autostart
```

## Конфигурация

При первом запуске создается файл:

```text
~/.config/voice-input/config.toml
```

Пример:

```toml
[hotkey]
key = "F12"
mode = "push_to_talk"

[audio]
device = ""
sample_rate = 16000

[whisper]
model_path = "~/.local/share/voice-input/models/ggml-large-v3.bin"
language = ""
beam_size = 5
initial_prompt = """
Мы обсуждаем программирование и архитектуру: React, TypeScript,
Docker, Kubernetes, microservices, middleware, observability.
"""

[output]
paste_delay_ms = 80
restore_primary = true
```

`language = ""` означает автоопределение языка. Для русской речи можно оставить
автоопределение или явно указать `language = "ru"`.

После изменения конфига закройте приложение через меню в трее и запустите его
заново.

## Отладочный запуск

Закройте уже запущенное приложение через меню в трее и запустите бинарник из
терминала:

```bash
~/.local/bin/voice-input
```

В терминале будет видно итоговый распознанный текст и ошибки.

## Файлы, которые не входят в репозиторий

В репозитории не хранятся:

- бинарники сборки (`bin/`);
- исходники и build-directory `whisper.cpp` (`third_party/`);
- модели Whisper (`models/`);
- локальные конфиги и `.env` файлы.

Это важно: модель большая, зависимости пересобираются локально, а локальные
настройки не должны попадать в публичный GitHub.

## Лицензия

MIT. Подробнее см. [LICENSE](LICENSE).
