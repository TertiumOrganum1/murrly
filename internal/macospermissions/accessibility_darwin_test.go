//go:build darwin

package macospermissions

import "testing"

// We cannot deterministically test trust state — it depends on user-level
// system settings. We can only verify the function returns without
// crashing the linker.
func TestIsAccessibilityTrustedCallable(t *testing.T) {
	_ = IsAccessibilityTrusted()
}
