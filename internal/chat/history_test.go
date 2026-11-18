package chat

import (
	"os"
	"path/filepath"
	"strings"
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
	rec := Record{Role: "assistant", Text: "ответ", Model: "deepseek-flash",
		Usage:      &provider.Usage{Input: 2000, Output: 300, CacheRead: 16000, Known: true},
		StopReason: "end",
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
	if *got[0].Usage != *rec.Usage || got[0].Model != rec.Model || got[0].Quotes[0] != rec.Quotes[0] {
		t.Fatalf("запись ассистента = %+v", got[0])
	}
	if got[1].Role != RoleTrim || got[1].Excerpts != 2 || got[1].Turns != 1 {
		t.Fatalf("запись trim = %+v", got[1])
	}
}

// Новые записи не несут ни профиля, ни цены, ни записи в кэш: даже если
// вызывающий по старой привычке их заполнил, Append их не пишет.
func TestAppendNeverWritesRetiredFields(t *testing.T) {
	h := History{Path: filepath.Join(t.TempDir(), "b.jsonl")}
	cost := 0.5
	err := h.Append(Record{Role: "assistant", Text: "ответ", Model: "m", Profile: "deepseek", CostUSD: &cost,
		Usage: &provider.Usage{Input: 1, Output: 2, Known: true}, StopReason: "end"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(h.Path)
	for _, key := range []string{`"profile"`, `"cost_usd"`, `"cache_write"`, "deepseek"} {
		if strings.Contains(string(data), key) {
			t.Errorf("в новой записи есть %s:\n%s", key, data)
		}
	}
	if !strings.Contains(string(data), `"model":"m"`) || !strings.Contains(string(data), `"usage"`) {
		t.Fatalf("потеряны живые поля:\n%s", data)
	}
}

// Журналы прежних версий — с профилем, ценой и записью в кэш — читаются как
// раньше: поля на месте (их читает расход, где они ничего не прибавляют), ход
// восстанавливается.
func TestOldJournalWithRetiredFieldsStillLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.jsonl")
	old := `{"time":"2026-11-10T10:00:00Z","role":"user","text":"что такое счастье?"}
{"time":"2026-11-10T10:00:05Z","role":"assistant","text":"Блаженство — деятельность души.","profile":"deepseek","model":"deepseek-v4-pro","usage":{"input":2000,"output":300,"cache_read":16000,"cache_write":120,"known":true},"cost_usd":0.0031,"stop_reason":"end"}
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := History{Path: path}.Load()
	if err != nil || len(got) != 2 {
		t.Fatalf("Load: %v, %d записей", err, len(got))
	}
	a := got[1]
	if a.Profile != "deepseek" || a.Model != "deepseek-v4-pro" || a.CostUSD == nil || *a.CostUSD != 0.0031 {
		t.Fatalf("старые поля не прочитаны: %+v", a)
	}
	if a.Usage == nil || a.Usage.CacheWrite != 120 || a.Usage.Prompt() != 2000+16000+120 || !a.Usage.Known {
		t.Fatalf("usage старой записи: %+v", a.Usage)
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
