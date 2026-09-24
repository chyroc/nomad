package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryEncodeDecode(t *testing.T) {
	cases := []string{
		"simple",
		"line one\nline two",
		`back\slash`,
		`a\b\nc`,
		"multi\nline\nthree",
	}
	for _, in := range cases {
		got := decodeHistoryEntry(encodeHistoryEntry(in))
		if got != in {
			t.Errorf("round-trip %q = %q", in, got)
		}
	}
}

func TestHistoryAppendDedupes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := appendHistoryEntry(path, nil, "first")
	h = appendHistoryEntry(path, h, "first")
	h = appendHistoryEntry(path, h, "second")
	if len(h) != 2 {
		t.Fatalf("history = %v, want 2 entries", h)
	}
	loaded := loadHistory(path)
	if len(loaded) != 2 || loaded[0] != "first" || loaded[1] != "second" {
		t.Fatalf("loaded history wrong: %v", loaded)
	}
}

func TestHistoryMultilinePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := appendHistoryEntry(path, nil, "line1\nline2")
	h = appendHistoryEntry(path, h, "line3")
	loaded := loadHistory(path)
	if len(loaded) != 2 || !strings.Contains(loaded[0], "line2") {
		t.Fatalf("multiline history lost: %v", loaded)
	}
}

func TestHistoryCapRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	var h []string
	for i := 0; i < maxHistoryEntries+5; i++ {
		h = appendHistoryEntry(path, h, "entry "+itoa(i))
	}
	if len(h) != maxHistoryEntries {
		t.Fatalf("history len = %d, want %d", len(h), maxHistoryEntries)
	}
	loaded := loadHistory(path)
	if len(loaded) != maxHistoryEntries {
		t.Fatalf("persisted len = %d", len(loaded))
	}
	if loaded[0] != "entry 5" || loaded[len(loaded)-1] != "entry "+itoa(maxHistoryEntries+4) {
		t.Fatalf("trim window wrong: first=%q last=%q", loaded[0], loaded[len(loaded)-1])
	}
}

func TestHistoryLoadMissing(t *testing.T) {
	if h := loadHistory(filepath.Join(t.TempDir(), "nope")); h != nil {
		t.Fatalf("missing file should yield nil, got %v", h)
	}
}
