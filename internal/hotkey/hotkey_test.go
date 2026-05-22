package hotkey

import "testing"

func TestEventConstantsDistinct(t *testing.T) {
	if EventDown == EventUp {
		t.Errorf("EventDown == EventUp; constants must be distinct")
	}
}
