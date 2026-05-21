package transcripthistory

import "testing"

func TestHistoryKeepsLatestItems(t *testing.T) {
	h := New(3)
	h.Add("first")
	h.Add("second")
	h.Add("third")
	h.Add("fourth")

	want := []string{"fourth", "third", "second"}
	got := h.Snapshot()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestHistoryGet(t *testing.T) {
	h := New(2)
	h.Add("latest")
	h.Add("new latest")

	if got, ok := h.Get(0); !ok || got != "new latest" {
		t.Fatalf("latest = %q, %v", got, ok)
	}
	if got, ok := h.Get(1); !ok || got != "latest" {
		t.Fatalf("previous = %q, %v", got, ok)
	}
	if _, ok := h.Get(2); ok {
		t.Fatal("unexpected third item")
	}
}

func TestHistorySkipsEmptyText(t *testing.T) {
	h := New(3)
	h.Add("")
	if got := h.Snapshot(); len(got) != 0 {
		t.Fatalf("items = %v", got)
	}
}
