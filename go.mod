module github.com/tertiumorganum1/voice-input

go 1.25.2

require (
	fyne.io/systray v1.12.1 // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/ggerganov/whisper.cpp/bindings/go v0.0.0-00010101000000-000000000000 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gordonklaus/portaudio v0.0.0-20260203164431-765aa7dfa631 // indirect
	github.com/robotn/gohook v0.42.3 // indirect
	github.com/vcaesar/keycode v0.10.1 // indirect
	golang.org/x/sys v0.15.0 // indirect
)

replace github.com/ggerganov/whisper.cpp/bindings/go => ./third_party/whisper.cpp/bindings/go
