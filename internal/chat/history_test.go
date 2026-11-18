package chat

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/provider"
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

func TestRecordNewFieldsRoundTrip(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "b.jsonl")}
	cost := 0.0031
	rec := Record{Role: "assistant", Text: "ответ", Profile: "deepseek", Model: "deepseek-v4-pro",
		Usage:   &provider.Usage{Input: 2000, Output: 300, CacheRead: 16000, Known: true},
		CostUSD: &cost, StopReason: "end",
		Quotes: []Quote{{Cite: "Nicomachean Ethics II.1", En: "moral virtue comes about as a result of habit",
			Ru: "нравственная добродетель возникает как следствие привычки", Verified: true,
			Chapter: "nicomachean-ethics/ii/01.md"}}}
	if err := h.Append(rec); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(Record{Role: RoleTrim, Excerpts: 2, Turns: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := h.Load()
	if err != nil || len(got) != 2 {
		t.Fatalf("Load: %v, %d записей", err, len(got))
	}
	if *got[0].Usage != *rec.Usage || *got[0].CostUSD != cost || got[0].Quotes[0] != rec.Quotes[0] {
		t.Fatalf("запись ассистента = %+v", got[0])
	}
	if got[1].Role != RoleTrim || got[1].Excerpts != 2 || got[1].Turns != 1 {
		t.Fatalf("запись trim = %+v", got[1])
	}
}

func TestTitleOf(t *testing.T) {
	long := "Почему люди поступают дурно, зная, что это дурно, и можно ли этому научиться избегать"
	recs := []Record{{Role: "user", Text: long}, {Role: "assistant", Text: "…"}}
	if got := TitleOf(recs); got != CutTitle(long) || len([]rune(got)) != 60 {
		t.Fatalf("заголовок по первому вопросу = %q", got)
	}
	recs = append(recs, Record{Role: RoleMeta, Title: "Акрасия"}, Record{Role: RoleMeta, Title: "О невоздержности"})
	if got := TitleOf(recs); got != "О невоздержности" {
		t.Fatalf("действует последняя meta: %q", got)
	}
	if TitleOf(nil) != "" {
		t.Fatal("пустая беседа без заголовка")
	}
}
