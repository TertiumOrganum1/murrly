//go:build darwin

// Package macospermissions wraps macOS Accessibility / TCC checks.
package macospermissions

/*
#cgo darwin LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>

static int mur_accessibility_trusted_with_prompt(int prompt) {
    const void* keys[1];
    const void* values[1];
    keys[0]   = kAXTrustedCheckOptionPrompt;
    values[0] = prompt ? kCFBooleanTrue : kCFBooleanFalse;
    CFDictionaryRef options = CFDictionaryCreate(
        kCFAllocatorDefault, keys, values, 1,
        &kCFCopyStringDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks);
    Boolean ok = AXIsProcessTrustedWithOptions(options);
    CFRelease(options);
    return ok ? 1 : 0;
}
*/
import "C"

// IsAccessibilityTrusted returns true if the calling process is trusted to
// receive system-wide input events (i.e. the user has enabled the app under
// System Settings → Privacy & Security → Accessibility). No prompt is shown.
func IsAccessibilityTrusted() bool {
	return C.mur_accessibility_trusted_with_prompt(0) == 1
}

// EnsureAccessibility checks the same trust status, but when not trusted it
// also asks the OS to display the standard system prompt that links to
// Settings. The function returns the current trust state — calling it once
// per app launch is sufficient.
func EnsureAccessibility() bool {
	return C.mur_accessibility_trusted_with_prompt(1) == 1
}
