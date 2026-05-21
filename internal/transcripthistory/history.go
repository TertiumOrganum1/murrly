// Package transcripthistory stores the latest recognized text chunks.
package transcripthistory

import "sync"

type History struct {
	mu    sync.Mutex
	limit int
	items []string
}

func New(limit int) *History {
	if limit <= 0 {
		limit = 1
	}
	return &History{limit: limit}
}

func (h *History) Add(text string) {
	if text == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	h.items = append([]string{text}, h.items...)
	if len(h.items) > h.limit {
		h.items = h.items[:h.limit]
	}
}

func (h *History) Get(index int) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if index < 0 || index >= len(h.items) {
		return "", false
	}
	return h.items[index], true
}

func (h *History) Snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.items))
	copy(out, h.items)
	return out
}
