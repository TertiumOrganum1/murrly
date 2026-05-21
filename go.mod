module github.com/tertiumorganum1/murrly

go 1.25.2

require (
	fyne.io/systray v1.12.1
	github.com/BurntSushi/toml v1.6.0
	github.com/ggerganov/whisper.cpp/bindings/go v0.0.0-00010101000000-000000000000
	github.com/gordonklaus/portaudio v0.0.0-20260203164431-765aa7dfa631
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
)

replace github.com/ggerganov/whisper.cpp/bindings/go => ./third_party/whisper.cpp/bindings/go
