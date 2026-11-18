package web

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/provider"
)

// writeRawJournal кладёт журнал строками как есть: так выглядят журналы
// прежних версий, которые chat.History.Append уже не напишет (профиль, цена,
// запись в кэш).
func writeRawJournal(t *testing.T, dir, id string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateByDayAndModel(t *testing.T) {
	dir := t.TempDir()
	day := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	writeJournal(t, dir, "2026-11-18T09-00-00",
		chat.Record{Time: day, Role: "assistant", Model: "deepseek-flash",
			Usage: &provider.Usage{Input: 100, Output: 10, CacheRead: 1000, Known: true}},
		chat.Record{Time: day.Add(time.Hour), Role: "assistant", Model: "deepseek-flash",
			Usage: &provider.Usage{Input: 50, Output: 5, Known: true}},
		// тот же день, другая модель — отдельная строка
		chat.Record{Time: day.Add(2 * time.Hour), Role: "assistant", Model: "deepseek-v4-pro",
			Usage: &provider.Usage{Input: 7, Output: 1, CacheRead: 3, Known: true}},
		chat.Record{Time: day.AddDate(0, 0, -40), Role: "assistant", Model: "deepseek-flash",
			Usage: &provider.Usage{Input: 999, Known: true}})
	rows, total, err := aggregate(dir, day.AddDate(0, 0, -1), day)
	if err != nil || len(rows) != 2 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
	// в одном дне — по названию модели
	flash, pro := rows[0], rows[1]
	if flash.Day != "2026-11-18" || flash.Model != "deepseek-flash" || flash.Input != 150 || flash.Output != 15 ||
		flash.CacheRead != 1000 || flash.Partial || flash.Answers != 2 {
		t.Fatalf("строка flash %+v", flash)
	}
	if pro.Model != "deepseek-v4-pro" || pro.Input != 7 || pro.CacheRead != 3 || pro.Answers != 1 {
		t.Fatalf("строка pro %+v", pro)
	}
	if total.Input != 157 || total.Output != 16 || total.CacheRead != 1003 || total.Answers != 3 || total.Partial {
		t.Fatalf("итог %+v", total)
	}
}

// Журналы прежних версий — с profile, cost_usd и usage.cache_write — читаются без
// ошибки; для убранных колонок они ничего не прибавляют, токены считаются как
// раньше, модель берётся из записи (а у записей без неё — пустая).
func TestAggregateReadsOldJournals(t *testing.T) {
	dir := t.TempDir()
	d := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local)
	ts := d.UTC().Format(time.RFC3339)
	writeRawJournal(t, dir, "2026-11-10T09-00-00",
		`{"time":"`+ts+`","role":"user","text":"вопрос"}`,
		`{"time":"`+ts+`","role":"assistant","text":"ответ","profile":"deepseek","model":"deepseek-v4-pro","usage":{"input":2000,"output":300,"cache_read":16000,"cache_write":120,"known":true},"cost_usd":0.0031,"stop_reason":"end"}`,
		// без модели и без профиля: журнал самой первой версии
		`{"time":"`+ts+`","role":"assistant","text":"ещё","usage":{"input":10,"output":2,"cache_read":0,"cache_write":0,"known":true}}`)
	rows, total, err := aggregate(dir, d, d)
	if err != nil || len(rows) != 2 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
	if rows[0].Model != "" || rows[0].Input != 10 || rows[1].Model != "deepseek-v4-pro" {
		t.Fatalf("строки %+v", rows)
	}
	old := rows[1]
	if old.Input != 2000 || old.Output != 300 || old.CacheRead != 16000 || old.Answers != 1 || old.Partial {
		t.Fatalf("старая строка %+v", old)
	}
	if total.Input != 2010 || total.Answers != 2 {
		t.Fatalf("итог %+v", total)
	}
	// В JSON строк нет ни цены, ни записи в кэш, ни профиля.
	raw, _ := json.Marshal(struct {
		Rows  []usageRow `json:"rows"`
		Total usageTotal `json:"total"`
	}{rows, total})
	for _, gone := range []string{"cost", "cache_write", "profile", "price"} {
		if strings.Contains(string(raw), gone) {
			t.Errorf("в расходе есть %q: %s", gone, raw)
		}
	}
}

// Ответ с known:false — оборванный ход или провайдер, не назвавший токенов:
// его числа входят в сумму, но сумма с ним неполная.
func TestAggregateMarksPartialRecords(t *testing.T) {
	dir := t.TempDir()
	d1 := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	d2 := d1.AddDate(0, 0, 1)
	writeJournal(t, dir, "2026-11-17T10-00-00",
		chat.Record{Time: d1, Role: "assistant", Model: "m1", Usage: &provider.Usage{Input: 10, Output: 2, Known: true}},
		// оборван: известные запросы хода посчитаны, итог неточный
		chat.Record{Time: d1.Add(time.Minute), Role: "assistant", Model: "m1", StopReason: "cancelled",
			Usage: &provider.Usage{Input: 7, Output: 1, Known: false}},
		// вовсе без чисел
		chat.Record{Time: d2, Role: "assistant", Model: "m2", StopReason: "error", Usage: &provider.Usage{}},
		// без usage (журнал командной строки до появления учёта) — не считается
		chat.Record{Time: d2, Role: "assistant", Model: "m2", Text: "старый ответ"},
		chat.Record{Time: d2, Role: "user", Text: "вопрос"})
	rows, total, err := aggregate(dir, d1, d2)
	if err != nil || len(rows) != 2 {
		t.Fatalf("строки %+v, %v", rows, err)
	}
	// по убыванию дня: сначала 18-е
	zero, mixed := rows[0], rows[1]
	if zero.Day != "2026-11-18" || zero.Model != "m2" || !zero.Partial || zero.Answers != 1 ||
		zero.Input+zero.Output+zero.CacheRead != 0 {
		t.Fatalf("строка без чисел %+v", zero)
	}
	if mixed.Day != "2026-11-17" || !mixed.Partial || mixed.Input != 17 || mixed.Output != 3 || mixed.Answers != 2 {
		t.Fatalf("смешанная строка %+v", mixed)
	}
	if !total.Partial || total.Answers != 3 || total.Input != 17 {
		t.Fatalf("итог %+v", total)
	}
}

// Контракт GET /api/usage: строки день × модель (model есть всегда, у журналов без
// модели — пустой), итог без дня и модели, ни цены, ни записи в кэш.
func TestUsageEndpointShape(t *testing.T) {
	s := newTestServer(t, nil, nil)
	now := time.Now()
	writeJournal(t, s.Paths.History, now.UTC().Format("2006-01-02T15-04-05"),
		chat.Record{Time: now, Role: "assistant", Model: "m1", Usage: &provider.Usage{Input: 5, Output: 1, CacheRead: 9, Known: true}},
		chat.Record{Time: now, Role: "assistant", Usage: &provider.Usage{Input: 2, Known: false}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/usage", "")
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	for _, gone := range []string{"cost", "cache_write", "profile"} {
		if strings.Contains(rec.Body.String(), gone) {
			t.Fatalf("в ответе есть %q: %s", gone, rec.Body.String())
		}
	}
	var raw struct {
		Rows  []map[string]any `json:"rows"`
		Total map[string]any   `json:"total"`
		From  string           `json:"from"`
		To    string           `json:"to"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Rows) != 2 {
		t.Fatalf("ответ %s, %v", rec.Body.String(), err)
	}
	keysOf := func(m map[string]any) string {
		var ks []string
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ",")
	}
	for _, row := range raw.Rows {
		if got := keysOf(row); got != "answers,cache_read,day,input,model,output,partial" {
			t.Fatalf("поля строки: %s", got)
		}
	}
	if got := keysOf(raw.Total); got != "answers,cache_read,input,output,partial" {
		t.Fatalf("поля итога: %s", got)
	}
	if raw.Rows[0]["model"] != "" || raw.Rows[1]["model"] != "m1" { // по возрастанию названия модели в одном дне
		t.Fatalf("строки %v", raw.Rows)
	}
	if raw.Total["input"] != 7.0 || raw.Total["answers"] != 2.0 || raw.Total["partial"] != true {
		t.Fatalf("итог %v", raw.Total)
	}
	if raw.From == "" || raw.To == "" {
		t.Fatalf("from/to: %+v", raw)
	}
}

func TestUsageEndpointRange(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	d := time.Date(2026, 3, 10, 12, 0, 0, 0, time.Local)
	writeJournal(t, s.Paths.History, "2026-03-10T12-00-00",
		chat.Record{Time: d, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 5, Known: true}})
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

// Каталога журналов нет — пустой расход, не ошибка; чужие файлы, чужие
// подкаталоги и всё, что глубже trash/, в расход не попадают.
func TestAggregateIgnoresStrayFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "нет-такого")
	if rows, total, err := aggregate(missing, time.Now(), time.Now()); err != nil || len(rows) != 0 || total.Answers != 0 {
		t.Fatalf("нет каталога: %+v, %+v, %v", rows, total, err)
	}
	dir := t.TempDir()
	d := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	rec := chat.Record{Time: d, Role: "assistant", Model: "x", Usage: &provider.Usage{Input: 1, Known: true}}
	os.WriteFile(filepath.Join(dir, "заметки.txt"), []byte("не журнал"), 0o600)
	os.WriteFile(filepath.Join(dir, "битый.jsonl"), []byte("{не json\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "trash"), 0o700)
	os.WriteFile(filepath.Join(dir, "trash", "заметки.txt"), []byte("не журнал"), 0o600)
	writeJournal(t, filepath.Join(dir, "archive"), "2026-11-18T09-00-00", rec)         // чужой подкаталог
	writeJournal(t, filepath.Join(dir, "trash", "deeper"), "2026-11-18T09-00-00", rec) // глубже trash/
	if rows, total, err := aggregate(dir, d, d); err != nil || len(rows) != 0 || total.Answers != 0 || total.Partial {
		t.Fatalf("строки %+v, итог %+v, %v", rows, total, err)
	}
}

// Удаление беседы не уменьшает расход: токены потрачены. Журнал из trash/
// считается наравне с остальными.
func TestAggregateCountsTrash(t *testing.T) {
	dir := t.TempDir()
	d := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	writeJournal(t, dir, "2026-11-18T09-00-00",
		chat.Record{Time: d, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 10, Known: true}})
	writeJournal(t, filepath.Join(dir, "trash"), "2026-11-18T08-00-00",
		chat.Record{Time: d, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 5, Known: true}})
	rows, total, err := aggregate(dir, d, d)
	if err != nil || len(rows) != 1 || rows[0].Input != 15 || rows[0].Answers != 2 || total.Input != 15 {
		t.Fatalf("строки %+v, итог %+v, %v", rows, total, err)
	}
}

// Сквозь API: беседу удалили — итог месяца в /api/state не просел.
func TestDeletingConversationKeepsMonthTotal(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	now := time.Now()
	id := now.UTC().Format("2006-01-02T15-04-05")
	writeJournal(t, s.Paths.History, id,
		chat.Record{Time: now, Role: "user", Text: "вопрос"},
		chat.Record{Time: now, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 100, Output: 20, Known: true}})
	month := func() usageTotal {
		var st struct {
			Month usageTotal `json:"month"`
		}
		if err := json.Unmarshal(do(t, h, http.MethodGet, "/api/state", "").Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		return st.Month
	}
	if m := month(); m.Input != 100 || m.Answers != 1 {
		t.Fatalf("до удаления %+v", m)
	}
	if rec := do(t, h, http.MethodDelete, "/api/conversations/"+id, ""); rec.Code != 200 {
		t.Fatalf("удаление: код %d", rec.Code)
	}
	if m := month(); m.Input != 100 || m.Output != 20 || m.Answers != 1 {
		t.Fatalf("после удаления итог месяца просел: %+v", m)
	}
}

// Журнал, который не прочитался (строка длиннее предела History.Load), не
// исчезает молча: в журнале сервера строка о нём без содержимого, а итог с
// пометкой «неполный». Прочитанное до сбоя и другие журналы считаются.
func TestAggregateMarksUnreadableJournalPartial(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	dir := t.TempDir()
	d := time.Date(2026, 11, 18, 12, 0, 0, 0, time.Local)
	writeJournal(t, dir, "2026-11-18T09-00-00",
		chat.Record{Time: d, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 10, Known: true}})
	// за пределом Scanner (8 МиБ) — ни одна запись до неё не теряется, остальное не читается
	f, err := os.OpenFile(filepath.Join(dir, "2026-11-18T10-00-00.jsonl"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	good, _ := json.Marshal(chat.Record{Time: d, Role: "assistant", Model: "m2", Usage: &provider.Usage{Input: 7, Known: true}})
	f.Write(append(good, '\n'))
	f.WriteString(strings.Repeat("СЕКРЕТНОЕСОДЕРЖИМОЕ", 9<<20/len("СЕКРЕТНОЕСОДЕРЖИМОЕ")+1) + "\n")
	f.Close()
	// журнал, начатый после конца периода, пометки не ставит
	writeJournal(t, dir, "2026-12-30T10-00-00", chat.Record{Time: d.AddDate(0, 1, 12), Role: "assistant", Usage: &provider.Usage{Input: 1, Known: true}})
	rows, total, err := aggregate(dir, d, d)
	if err != nil {
		t.Fatal(err)
	}
	if !total.Partial || total.Input != 17 || len(rows) != 2 {
		t.Fatalf("строки %+v, итог %+v", rows, total)
	}
	if !strings.Contains(logs.String(), "2026-11-18T10-00-00.jsonl") || strings.Contains(logs.String(), "СЕКРЕТНОЕ") {
		t.Fatalf("журнал сервера: %.300q", logs.String())
	}
	// а для периода раньше начала этого журнала пометки нет
	if _, total, _ := aggregate(dir, d.AddDate(0, -3, 0), d.AddDate(0, -2, 0)); total.Partial {
		t.Errorf("пометка у периода, до которого журнал не доживает: %+v", total)
	}
}

func TestStateCarriesMonthTotal(t *testing.T) {
	s := newTestServer(t, nil, nil)
	now := time.Now()
	writeJournal(t, s.Paths.History, now.UTC().Format("2006-01-02T15-04-05"),
		chat.Record{Time: now, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 100, Output: 20, CacheRead: 300, Known: true}},
		chat.Record{Time: now, Role: "assistant", Model: "deepseek-flash", Usage: &provider.Usage{Input: 3, Known: false}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/state", "")
	var st struct {
		Month usageTotal `json:"month"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	m := st.Month
	if m.Input != 103 || m.Output != 20 || m.CacheRead != 300 || m.Answers != 2 || !m.Partial {
		t.Fatalf("итог за месяц %+v", m)
	}
}
