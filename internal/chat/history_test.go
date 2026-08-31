package chat

import (
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryRoundTrip(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "беседа.jsonl")}
	in := []Record{
		{Time: time.Unix(1, 0).UTC(), Role: "user", Text: "что такое счастье"},
		{Time: time.Unix(2, 0).UTC(), Role: "assistant", Text: "деятельность души"},
	}
	for _, r := range in {
		if err := h.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	out, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Text != "деятельность души" {
		t.Fatalf("%+v", out)
	}
}

func TestHistoryLoadMissingIsEmpty(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "нет.jsonl")}
	out, err := h.Load()
	if err != nil {
		t.Fatalf("отсутствие файла не ошибка: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestHistorySurvivesNewlinesInText(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "б.jsonl")}
	if err := h.Append(Record{Role: "assistant", Text: "первая\nвторая"}); err != nil {
		t.Fatal(err)
	}
	out, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Text != "первая\nвторая" {
		t.Fatalf("перевод строки внутри записи сломал JSONL: %+v", out)
	}
}
