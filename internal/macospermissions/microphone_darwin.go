//go:build darwin

package macospermissions

/*
#cgo darwin LDFLAGS: -framework Foundation -framework AVFoundation -framework CoreMedia
#include "microphone_darwin.h"
*/
import "C"

// MicrophoneAuthStatus reports the current TCC decision for our mic
// access. Returned values mirror AVAuthorizationStatus:
//
//	0 — NotDetermined (no prompt has been answered yet)
//	1 — Restricted    (parental controls / MDM)
//	2 — Denied
//	3 — Authorized
//
// To actually surface the prompt, call recorder.Probe — macOS only
// shows it when the CoreAudio stack (AUHAL) tries to start, not when
// AVCaptureDevice merely queries status.
func MicrophoneAuthStatus() int {
	return int(C.mur_microphone_authorization_status())
}

// IsMicrophoneAuthorized reports whether the user has granted mic
// access. NotDetermined and Denied both return false — only an
// explicit "Authorized" counts.
func IsMicrophoneAuthorized() bool {
	return C.mur_microphone_authorization_status() == 3
}
