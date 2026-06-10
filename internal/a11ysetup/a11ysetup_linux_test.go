//go:build linux

package a11ysetup

import (
	"strings"
	"testing"
)

func TestVscodeAccessibilityOn(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"missing key", `{"editor.fontSize": 14}`, false},
		{"on", `{"editor.accessibilitySupport": "on"}`, true},
		{"off", `{"editor.accessibilitySupport": "off"}`, false},
		{"auto", `{"editor.accessibilitySupport": "auto"}`, false},
		{"spaced", `{ "editor.accessibilitySupport" :   "on" }`, true},
		{"empty file", ``, false},
	}
	for _, tc := range cases {
		if got := vscodeAccessibilityOn([]byte(tc.src)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPatchVSCodeSettings(t *testing.T) {
	t.Run("inserts into populated object preserving content", func(t *testing.T) {
		src := "{\n    // window\n    \"window.zoomLevel\": 1,\n    \"editor.fontSize\": 14\n}\n"
		out, changed, err := patchVSCodeSettings([]byte(src))
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		s := string(out)
		if !strings.Contains(s, `"editor.accessibilitySupport": "on",`) {
			t.Fatalf("key not inserted:\n%s", s)
		}
		if !strings.Contains(s, "// window") || !strings.Contains(s, `"editor.fontSize": 14`) {
			t.Fatalf("original content damaged:\n%s", s)
		}
		if !vscodeAccessibilityOn(out) {
			t.Fatalf("patched file not detected as on")
		}
	})

	t.Run("replaces existing off value", func(t *testing.T) {
		src := `{"editor.accessibilitySupport": "off", "editor.fontSize": 14}`
		out, changed, err := patchVSCodeSettings([]byte(src))
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if !vscodeAccessibilityOn(out) {
			t.Fatalf("value not replaced: %s", out)
		}
		if !strings.Contains(string(out), `"editor.fontSize": 14`) {
			t.Fatalf("sibling key damaged: %s", out)
		}
	})

	t.Run("already on is a no-op", func(t *testing.T) {
		src := `{"editor.accessibilitySupport": "on"}`
		out, changed, err := patchVSCodeSettings([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Fatalf("unexpected change: %s", out)
		}
	})

	t.Run("empty object gets key without trailing comma", func(t *testing.T) {
		out, changed, err := patchVSCodeSettings([]byte("{}"))
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if strings.Contains(string(out), `"on",`) {
			t.Fatalf("trailing comma in empty object: %s", out)
		}
		if !vscodeAccessibilityOn(out) {
			t.Fatalf("not detected as on: %s", out)
		}
	})

	t.Run("empty file becomes minimal object", func(t *testing.T) {
		out, changed, err := patchVSCodeSettings([]byte("  \n"))
		if err != nil || !changed {
			t.Fatalf("changed=%v err=%v", changed, err)
		}
		if !vscodeAccessibilityOn(out) {
			t.Fatalf("not detected as on: %s", out)
		}
	})

	t.Run("garbage without object errors out", func(t *testing.T) {
		if _, _, err := patchVSCodeSettings([]byte("not json at all")); err == nil {
			t.Fatal("want error")
		}
	})
}
