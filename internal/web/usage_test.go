package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/provider"
)

func TestAggregateByDayAndProfile(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	cost := 0.01
	writeJournal(t, dir, "2026-11-18T09-00-00",
		chat.Record{Time: day, Role: "assistant", Profile: "deepseek",
			Usage: &provider.Usage{Input: 100, Output: 10, CacheRead: 1000, Known: true}, CostUSD: &cost},
		chat.Record{Time: day.Add(time.Hour), Role: "assistant", Profile: "deepseek",
			Usage: &provider.Usage{Input: 50, Output: 5, Known: true}},
		chat.Record{Time: day.AddDate(0, 0, -40), Role: "assistant", Profile: "deepseek",
			Usage: &provider.Usage{Input: 999, Known: true}})
	rows, total, err := aggregate(dir, day.AddDate(0, 0, -1), day)
	if err != nil || len(rows) != 1 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
	r := rows[0]
	if r.Day != "2026-11-18" || r.Input != 150 || r.Output != 15 || r.CacheRead != 1000 ||
		r.CostUSD == nil || *r.CostUSD != 0.01 || !r.CostPartial || r.Partial || r.Answers != 2 {
		t.Fatalf("строка %+v", r)
	}
	if total.Input != 150 || !total.CostPartial || total.Partial {
		t.Fatalf("итог %+v", total)
	}
}

// Ответ с known:false — оборванный ход или провайдер, не назвавший токенов:
// его числа входят в сумму, но сумма с ним неполная.
func TestAggregateMarksPartialRecords(t *testing.T) {
	dir := t.TempDir()
	d1 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	d2 := d1.AddDate(0, 0, 1)
	cost := 0.5
	writeJournal(t, dir, "2026-11-17T10-00-00",
		chat.Record{Time: d1, Role: "assistant", Profile: "deepseek", Usage: &provider.Usage{Input: 10, Output: 2, Known: true}, CostUSD: &cost},
		// оборван: известные запросы хода посчитаны, итог неточный
		chat.Record{Time: d1.Add(time.Minute), Role: "assistant", Profile: "deepseek", StopReason: "cancelled",
			Usage: &provider.Usage{Input: 7, Output: 1, Known: false}},
		// вовсе без чисел
		chat.Record{Time: d2, Role: "assistant", Profile: "claude", StopReason: "error", Usage: &provider.Usage{}},
		// без usage (журнал командной строки до появления учёта) — не считается
		chat.Record{Time: d2, Role: "assistant", Profile: "claude", Text: "старый ответ"},
		chat.Record{Time: d2, Role: "user", Text: "вопрос"})
	rows, total, err := aggregate(dir, d1, d2)
	if err != nil || len(rows) != 2 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
	// по убыванию дня: сначала 18-е
	zero, mixed := rows[0], rows[1]
	if zero.Day != "2026-11-18" || zero.Profile != "claude" || !zero.Partial || zero.Answers != 1 ||
		zero.Input+zero.Output+zero.CacheRead+zero.CacheWrite != 0 || zero.CostUSD != nil {
		t.Fatalf("строка без чисел %+v", zero)
	}
	if mixed.Day != "2026-11-17" || !mixed.Partial || mixed.Input != 17 || mixed.Output != 3 || mixed.Answers != 2 ||
		mixed.CostUSD == nil || *mixed.CostUSD != 0.5 || !mixed.CostPartial {
		t.Fatalf("смешанная строка %+v", mixed)
	}
	if !total.Partial || total.Answers != 3 || total.Input != 17 {
		t.Fatalf("итог %+v", total)
	}
}

// Нет ни одной цены — cost_usd в ответе нет вовсе, а не 0: «$» не показывается.
func TestUsageHasNoCostWithoutPrices(t *testing.T) {
	s := newTestServer(t, nil, nil)
	now := time.Now()
	writeJournal(t, s.Paths.History, now.UTC().Format("2006-01-02T15-04-05"),
		chat.Record{Time: now, Role: "assistant", Profile: "deepseek", Usage: &provider.Usage{Input: 5, Output: 1, Known: true}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/usage", "")
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "cost_usd") {
		t.Fatalf("cost_usd в ответе без цен: %s", rec.Body.String())
	}
	var out struct {
		Rows  []usageRow `json:"rows"`
		Total usageRow   `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Rows) != 1 || out.Total.Answers != 1 {
		t.Fatalf("ответ %s, %v", rec.Body.String(), err)
	}
}

func TestUsageEndpointRange(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	d := time.Date(2026, 3, 10, 12, 0, 0, 0, time.Local)
	writeJournal(t, s.Paths.History, "2026-03-10T12-00-00",
		chat.Record{Time: d, Role: "assistant", Profile: "deepseek", Usage: &provider.Usage{Input: 5, Known: true}})
	var out struct {
		Rows     []usageRow `json:"rows"`
		From, To string
	}
	rec := do(t, h, http.MethodGet, "/api/usage?from=2026-03-01&to=2026-03-31", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 || len(out.Rows) != 1 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/api/usage?from=2026-04-01&to=2026-04-30", "")
	out.Rows = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Rows) != 0 {
		t.Fatalf("за апрель строки %+v, %v", out.Rows, err)
	}
	for _, q := range []string{"from=вчера", "to=2026-13-01", "from=2026-03-31&to=2026-03-01", "from=2026-3-1"} {
		if rec := do(t, h, http.MethodGet, "/api/usage?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: код %d, ожидался 400", q, rec.Code)
		}
	}
}

// Каталога журналов нет — пустой расход, не ошибка; чужие файлы и корзина
// в расход не попадают.
func TestAggregateIgnoresStrayFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "нет-такого")
	if rows, total, err := aggregate(missing, time.Now(), time.Now()); err != nil || len(rows) != 0 || total.Answers != 0 {
		t.Fatalf("нет каталога: %+v, %+v, %v", rows, total, err)
	}
	dir := t.TempDir()
	d := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	os.WriteFile(filepath.Join(dir, "заметки.txt"), []byte("не журнал"), 0o600)
	os.WriteFile(filepath.Join(dir, "битый.jsonl"), []byte("{не json\n"), 0o600)
	writeJournal(t, filepath.Join(dir, "trash"), "2026-11-18T09-00-00",
		chat.Record{Time: d, Role: "assistant", Profile: "x", Usage: &provider.Usage{Input: 1, Known: true}})
	if rows, _, err := aggregate(dir, d, d); err != nil || len(rows) != 0 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
}

func TestStateCarriesMonthTotal(t *testing.T) {
	s := newTestServer(t, nil, nil)
	now := time.Now()
	cost := 0.25
	writeJournal(t, s.Paths.History, now.UTC().Format("2006-01-02T15-04-05"),
		chat.Record{Time: now, Role: "assistant", Profile: "deepseek", Usage: &provider.Usage{Input: 100, Output: 20, CacheRead: 300, Known: true}, CostUSD: &cost},
		chat.Record{Time: now, Role: "assistant", Profile: "deepseek", Usage: &provider.Usage{Input: 3, Known: false}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/state", "")
	var st struct {
		Month usageRow `json:"month"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	m := st.Month
	if m.Input != 103 || m.Output != 20 || m.CacheRead != 300 || m.Answers != 2 || !m.Partial ||
		m.CostUSD == nil || *m.CostUSD != 0.25 {
		t.Fatalf("итог за месяц %+v", m)
	}
}
