package tray

import "testing"

func TestTranscriptPreviewCompactsWhitespace(t *testing.T) {
	got := transcriptPreview("  one\n\n two\tthree  ", 20)
	if got != "one two three" {
		t.Fatalf("preview = %q", got)
	}
}

func TestTranscriptPreviewTruncatesRunes(t *testing.T) {
	got := transcriptPreview("привет мир", 6)
	if got != "привет..." {
		t.Fatalf("preview = %q", got)
	}
}

func TestTranscriptMenuTitle(t *testing.T) {
	got := transcriptMenuTitle(0, "recognized text")
	if got != "Copy latest: recognized text" {
		t.Fatalf("title = %q", got)
	}
}
