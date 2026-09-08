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

// fakeRow records every write, so a test can assert on how much the applet
// would have been told to redraw.
type fakeRow struct {
	title   string
	visible bool
	writes  []string
}

func (r *fakeRow) SetTitle(s string) { r.title = s; r.writes = append(r.writes, "title:"+s) }
func (r *fakeRow) Enable()           { r.writes = append(r.writes, "enable") }
func (r *fakeRow) Show()             { r.visible = true; r.writes = append(r.writes, "show") }
func (r *fakeRow) Hide()             { r.visible = false; r.writes = append(r.writes, "hide") }

func newFakeSlots(n int) (*transcriptSlots, []*fakeRow) {
	rows := make([]*fakeRow, n)
	items := make([]menuRow, n)
	for i := range rows {
		rows[i] = &fakeRow{}
		items[i] = rows[i]
	}
	return &transcriptSlots{items: items, shown: make([]string, n)}, rows
}

func totalWrites(rows []*fakeRow) int {
	n := 0
	for _, r := range rows {
		n += len(r.writes)
	}
	return n
}

// A new phrase shifts every row's text down by one, so every row legitimately
// changes — but rows that were already visible must only get a new title, not
// another show, and rows past the end must stay untouched.
func TestTranscriptSlotsWriteOnlyWhatChanged(t *testing.T) {
	slots, rows := newFakeSlots(5)

	slots.render([]string{"первая", "вторая"})
	if !rows[0].visible || !rows[1].visible {
		t.Fatal("filled rows were not shown")
	}
	if rows[2].visible || len(rows[2].writes) != 0 {
		t.Errorf("empty row was touched: %v", rows[2].writes)
	}

	// Re-rendering the same list must be a no-op — this is the case that used
	// to cost a full menu rebuild every time the filter toggled.
	for _, r := range rows {
		r.writes = nil
	}
	slots.render([]string{"первая", "вторая"})
	if n := totalWrites(rows); n != 0 {
		t.Errorf("unchanged render made %d writes, want 0", n)
	}

	// One new phrase at the top: rows 0-1 get new titles, row 2 appears.
	for _, r := range rows {
		r.writes = nil
	}
	slots.render([]string{"третья", "первая", "вторая"})
	if got := rows[0].writes; len(got) != 1 || got[0] != "title:третья" {
		t.Errorf("row 0 writes = %v, want a single title change", got)
	}
	if got := rows[3].writes; len(got) != 0 {
		t.Errorf("row 3 writes = %v, want none", got)
	}
	if n := totalWrites(rows); n != 5 {
		t.Errorf("total writes = %d, want 5 (two titles, one title+enable+show)", n)
	}
}

// A shorter list hides the rows that fell off, once each.
func TestTranscriptSlotsHideDroppedRowsOnce(t *testing.T) {
	slots, rows := newFakeSlots(3)
	slots.render([]string{"a", "b", "c"})
	for _, r := range rows {
		r.writes = nil
	}

	slots.render([]string{"a"})
	if rows[1].visible || rows[2].visible {
		t.Fatal("dropped rows stayed visible")
	}
	slots.render([]string{"a"})
	for i := 1; i < 3; i++ {
		if n := len(rows[i].writes); n != 1 {
			t.Errorf("row %d writes = %v, want a single hide", i, rows[i].writes)
		}
	}
}
