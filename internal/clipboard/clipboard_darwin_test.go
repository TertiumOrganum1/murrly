//go:build darwin

package clipboard

import (
	"os/exec"
	"testing"
)

func skipIfNoPbcopy(t *testing.T) {
	if _, err := exec.LookPath("pbcopy"); err != nil {
		t.Skip("pbcopy not in PATH")
	}
}

func TestSetGet(t *testing.T) {
	skipIfNoPbcopy(t)
	c := New()
	if err := c.Set("hello murrly"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Save()
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got.Text != "hello murrly" {
		t.Errorf("Save.Text = %q, want %q", got.Text, "hello murrly")
	}
	if !got.HasContent {
		t.Errorf("Save.HasContent = false, want true")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	skipIfNoPbcopy(t)
	c := New()
	_ = c.Set("original")
	saved, _ := c.Save()
	_ = c.Set("transient")
	if err := c.Restore(saved); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, _ := c.Save()
	if got.Text != "original" {
		t.Errorf("after restore, clipboard = %q, want %q", got.Text, "original")
	}
}
