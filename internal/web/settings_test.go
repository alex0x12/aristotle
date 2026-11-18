package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/provider"
)

var cyrillicRe = regexp.MustCompile(`[а-яА-ЯёЁ]`)

func getSettings(t *testing.T, h http.Handler) settingsDoc {
	t.Helper()
	rec := do(t, h, http.MethodGet, "/api/settings", "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/settings: код %d: %s", rec.Code, rec.Body.String())
	}
	var doc settingsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func putSettings(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, h, http.MethodPut, "/api/settings", body)
}

func jsonOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("не JSON: %v: %s", err, rec.Body.String())
	}
	return m
}

func keysOfMap(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

// ---- GET /api/settings, GET /api/state: контракт ----

func TestSettingsContractWithKey(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	m := jsonOf(t, do(t, h, http.MethodGet, "/api/settings", ""))
	// Ровно эти поля: ни профилей, ни default, ни corpus_k, ни цен; у здорового
	// конфига нет degraded, warning и backup.
	if got := keysOfMap(m); got != "base_url,context_budget,key,model" {
		t.Fatalf("поля настроек: %s", got)
	}
	if m["base_url"] != "https://api.deepseek.com/v1" || m["model"] != "deepseek-flash" || m["context_budget"] != 128000.0 {
		t.Fatalf("умолчания: %v", m)
	}
	key := m["key"].(map[string]any)
	if key["set"] != true || key["hint"] != "…4f1d" || keysOfMap(key) != "hint,set" {
		t.Fatalf("ключ: %v", key)
	}

	st := jsonOf(t, do(t, h, http.MethodGet, "/api/state", ""))
	if got := keysOfMap(st); got != "base_url,context_budget,key,model,month" {
		t.Fatalf("поля состояния: %s", got)
	}
	if st["model"] != m["model"] || st["base_url"] != m["base_url"] || st["context_budget"] != m["context_budget"] {
		t.Fatalf("состояние: %v", st)
	}
	if got := keysOfMap(st["month"].(map[string]any)); got != "answers,cache_read,input,output,partial" {
		t.Fatalf("поля month: %s", got)
	}
}

func TestSettingsContractWithoutKey(t *testing.T) {
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	for _, path := range []string{"/api/settings", "/api/state"} {
		m := jsonOf(t, do(t, s.Handler(), http.MethodGet, path, ""))
		key := m["key"].(map[string]any)
		if key["set"] != false || keysOfMap(key) != "set" {
			t.Fatalf("%s: ключ без ключа: %v", path, key)
		}
	}
}

// Старый keys.json ({"profiles": …}) — «ключа нет», без предупреждения.
func TestOldFormatKeysFileMeansNoKeyWithoutWarning(t *testing.T) {
	s := newTestServer(t, nil, nil)
	os.WriteFile(s.Paths.Keys, []byte(`{"profiles":{"deepseek":"OLD-FAKE-KEY-8888"}}`), 0o600)
	h := s.Handler()
	for _, path := range []string{"/api/settings", "/api/state"} {
		rec := do(t, h, http.MethodGet, path, "")
		key := jsonOf(t, rec)["key"].(map[string]any)
		if key["set"] != false || keysOfMap(key) != "set" || strings.Contains(rec.Body.String(), "OLD-FAKE") {
			t.Fatalf("%s: %v", path, key)
		}
	}
	// Новый ключ заменяет файл старого формата.
	if rec := do(t, h, http.MethodPut, "/api/key", `{"key":"NEW-FAKE-KEY-1111"}`); rec.Code != 200 {
		t.Fatalf("PUT: код %d: %s", rec.Code, rec.Body.String())
	}
	if k, err := s.keyStore().Get(); err != nil || k != "NEW-FAKE-KEY-1111" {
		t.Fatalf("ключ %q, %v", k, err)
	}
}

// ---- PUT /api/settings ----

func TestPutSettingsSavesAndReturnsDocument(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	rec := putSettings(t, h, `{"base_url":"https://openrouter.ai/api/v1","model":"anthropic/claude-opus-5","context_budget":32000}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	m := jsonOf(t, rec)
	if got := keysOfMap(m); got != "base_url,context_budget,key,model" {
		t.Fatalf("поля ответа: %s", got)
	}
	if m["base_url"] != "https://openrouter.ai/api/v1" || m["model"] != "anthropic/claude-opus-5" || m["context_budget"] != 32000.0 {
		t.Fatalf("ответ: %v", m)
	}
	// Файл — три поля, как их пишет config.Save; Load читает обратно то же.
	cfg, err := config.Load(s.Paths.Config)
	if err != nil || cfg != (config.Config{BaseURL: "https://openrouter.ai/api/v1", Model: "anthropic/claude-opus-5", ContextBudget: 32000}) {
		t.Fatalf("конфиг на диске %+v, %v", cfg, err)
	}
	data, _ := os.ReadFile(s.Paths.Config)
	var onDisk map[string]any
	json.Unmarshal(data, &onDisk)
	if keysOfMap(onDisk) != "base_url,context_budget,model" {
		t.Fatalf("поля файла: %s", data)
	}
	st, _ := os.Stat(s.Paths.Config)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("права config.json %v", st.Mode().Perm())
	}
	// GET видит то же; следующий ход идёт с новым подключением (см. TestTurnUsesSavedSettings).
	if got := getSettings(t, h); got.BaseURL != "https://openrouter.ai/api/v1" || got.ContextBudget != 32000 || got.Model != "anthropic/claude-opus-5" {
		t.Fatalf("GET после PUT: %+v", got)
	}
}

func TestPutSettingsTrimsAndAcceptsBoundaries(t *testing.T) {
	long := strings.Repeat("я", 200)
	for name, c := range map[string]struct {
		body string
		want config.Config
	}{
		"пробелы срезаются": {`{"base_url":"  http://127.0.0.1:11434/v1 ","model":"  qwen3.5:4b ","context_budget":1000}`,
			config.Config{BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen3.5:4b", ContextBudget: 1000}},
		"бюджет 0 — без урезания": {`{"base_url":"https://a.example/v1","model":"m","context_budget":0}`,
			config.Config{BaseURL: "https://a.example/v1", Model: "m", ContextBudget: 0}},
		"бюджет на пределе": {`{"base_url":"https://a.example/v1","model":"m","context_budget":10000000}`,
			config.Config{BaseURL: "https://a.example/v1", Model: "m", ContextBudget: 10_000_000}},
		"модель 200 знаков": {`{"base_url":"https://a.example/v1","model":"` + long + `","context_budget":1}`,
			config.Config{BaseURL: "https://a.example/v1", Model: long, ContextBudget: 1}},
		"адрес 2048 знаков": {`{"base_url":"https://a.example/` + strings.Repeat("a", 2048-len("https://a.example/")) + `","model":"m","context_budget":1}`,
			config.Config{BaseURL: "https://a.example/" + strings.Repeat("a", 2048-len("https://a.example/")), Model: "m", ContextBudget: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, nil, nil)
			if rec := putSettings(t, s.Handler(), c.body); rec.Code != 200 {
				t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
			}
			if got, err := config.Load(s.Paths.Config); err != nil || got != c.want {
				t.Fatalf("на диске %+v, %v", got, err)
			}
		})
	}
}

// (d) Таблица проверок: каждое правило — 400 по-русски с названием поля, и ни
// файла, ни копии на диске.
func TestPutSettingsValidationTable(t *testing.T) {
	good := func(over string) string {
		m := map[string]string{"base_url": `"https://api.example.com/v1"`, "model": `"m"`, "context_budget": `10`}
		for _, kv := range strings.Split(over, ";") {
			if kv == "" {
				continue
			}
			k, v, _ := strings.Cut(kv, "=")
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		var parts []string
		for k, v := range m {
			parts = append(parts, fmt.Sprintf("%q:%s", k, v))
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ",") + "}"
	}
	rows := []struct{ name, body, field string }{
		// base_url
		{"адрес пустой", good(`base_url="";`), "base_url"},
		{"адрес из пробелов", good(`base_url="   "`), "base_url"},
		{"адрес без схемы", good(`base_url="api.example.com/v1"`), "base_url"},
		{"адрес ftp", good(`base_url="ftp://api.example.com"`), "base_url"},
		{"адрес javascript", good(`base_url="javascript:alert(1)"`), "base_url"},
		{"адрес без хоста", good(`base_url="https://"`), "base_url"},
		{"адрес с логином", good(`base_url="https://user:pw@api.example.com/v1"`), "base_url"},
		{"адрес с пробелом", good(`base_url="https://api.example.com/a b"`), "base_url"},
		{"адрес с переводом строки", good(`base_url="https://api.example.com/a\nb"`), "base_url"},
		{"адрес 2049 знаков", good(`base_url="https://a.example/` + strings.Repeat("a", 2049-len("https://a.example/")) + `"`), "base_url"},
		{"адрес не строка", good(`base_url=42`), "base_url"},
		{"адреса нет в теле", good(`base_url=`), "base_url"},
		{"адрес null", good(`base_url=null`), "base_url"},
		// model
		{"модель пустая", good(`model=""`), "model"},
		{"модель из пробелов", good(`model="   "`), "model"},
		{"модель 201 знак", good(`model="` + strings.Repeat("a", 201) + `"`), "model"},
		{"модель с переводом строки", good(`model="a\nb"`), "model"},
		{"модель с NUL", good(`model="a\u0000b"`), "model"},
		{"модель не строка", good(`model=true`), "model"},
		{"модели нет в теле", good(`model=`), "model"},
		// context_budget
		{"бюджет отрицательный", good(`context_budget=-1`), "context_budget"},
		{"бюджет выше предела", good(`context_budget=10000001`), "context_budget"},
		{"бюджет строкой", good(`context_budget="64000"`), "context_budget"},
		{"бюджет дробный", good(`context_budget=1.5`), "context_budget"},
		{"бюджет огромный", good(`context_budget=99999999999999999999`), "context_budget"},
		{"бюджета нет в теле", good(`context_budget=`), "context_budget"},
		// чужие поля: профили и всё, что было настройкой раньше
		{"profile", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"profile":"x"}`, "profile"},
		{"profiles", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"profiles":[]}`, "profiles"},
		{"default", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"default":"x"}`, "default"},
		{"corpus_k", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"corpus_k":6}`, "corpus_k"},
		{"key", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"key":"x"}`, "key"},
		{"temperature", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"temperature":0.5}`, "temperature"},
		{"prices", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"prices":{}}`, "prices"},
		{"reasoning", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"reasoning":true}`, "reasoning"},
		{"removed", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"removed":["x"]}`, "removed"},
		{"degraded", `{"base_url":"https://a.example/v1","model":"m","context_budget":1,"degraded":true}`, "degraded"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			s := newTestServer(t, nil, nil)
			rec := putSettings(t, s.Handler(), r.body)
			msg := rec.Body.String()
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("код %d: %s", rec.Code, msg)
			}
			if !strings.Contains(msg, r.field) || !cyrillicRe.MatchString(msg) {
				t.Fatalf("сообщение должно быть по-русски и называть поле %q: %s", r.field, msg)
			}
			noPathNoParser(t, r.name, msg, s)
			for _, f := range []string{s.Paths.Config} {
				if _, err := os.Stat(f); !os.IsNotExist(err) {
					t.Fatalf("файл создан несмотря на отказ: %v", err)
				}
			}
		})
	}
}

// Тело не JSON, пустое, с лишним после объекта и слишком большое — отказ до диска.
func TestPutSettingsRejectsBadBodies(t *testing.T) {
	huge := `{"base_url":"https://a.example/v1","model":"` + strings.Repeat("a", maxSettingsBody) + `","context_budget":1}`
	for name, c := range map[string]struct {
		body string
		code int
	}{
		"пустое":          {"", 400},
		"не JSON":         {"{не json", 400},
		"массив":          {"[]", 400},
		"лишнее после":    {`{"base_url":"https://a.example/v1","model":"m","context_budget":1} {}`, 400},
		"слишком большое": {huge, 413},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, nil, nil)
			rec := putSettings(t, s.Handler(), c.body)
			if rec.Code != c.code || !cyrillicRe.MatchString(rec.Body.String()) {
				t.Fatalf("код %d: %.200s", rec.Code, rec.Body.String())
			}
			if _, err := os.Stat(s.Paths.Config); !os.IsNotExist(err) {
				t.Fatalf("файл создан: %v", err)
			}
		})
	}
}

// Недопустимое сохранение существующий файл не трогает.
func TestPutSettingsInvalidKeepsExistingFile(t *testing.T) {
	s := newTestServer(t, nil, nil)
	const before = `{"base_url":"https://a.example/v1","model":"keep-me","context_budget":5}`
	writeConfig(t, s, before)
	if rec := putSettings(t, s.Handler(), `{"base_url":"ftp://x","model":"m","context_budget":1}`); rec.Code != 400 {
		t.Fatalf("код %d", rec.Code)
	}
	if data, _ := os.ReadFile(s.Paths.Config); string(data) != before {
		t.Fatalf("файл изменён: %s", data)
	}
	if m, _ := filepath.Glob(s.Paths.Config + ".bad-*"); len(m) != 0 {
		t.Fatalf("копия при отказе: %v", m)
	}
}

// ---- ключ: PUT/DELETE /api/key ----

func TestKeyEndpoints(t *testing.T) {
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	h := s.Handler()

	rec := do(t, h, http.MethodPut, "/api/key", `{"key":"  FAKE-KEY-EP-q1w2e3-a1b2  "}`)
	if rec.Code != 200 {
		t.Fatalf("PUT: код %d: %s", rec.Code, rec.Body.String())
	}
	m := jsonOf(t, rec)
	key := m["key"].(map[string]any)
	if keysOfMap(m) != "key,ok" || m["ok"] != true || key["set"] != true || key["hint"] != "…a1b2" || keysOfMap(key) != "hint,set" {
		t.Fatalf("ответ PUT: %v", m)
	}
	if strings.Contains(rec.Body.String(), "FAKE-KEY-EP") {
		t.Fatalf("ключ в ответе: %s", rec.Body.String())
	}
	if k, _ := s.keyStore().Get(); k != "FAKE-KEY-EP-q1w2e3-a1b2" {
		t.Fatalf("сохранён %q (пробелы по краям должны срезаться)", k)
	}
	st, err := os.Stat(s.Paths.Keys)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права keys.json: %v, %v", st.Mode().Perm(), err)
	}
	if got := getSettings(t, h); !got.Key.Set || got.Key.Hint != "…a1b2" {
		t.Fatalf("настройки после PUT: %+v", got.Key)
	}

	// Замена
	if rec := do(t, h, http.MethodPut, "/api/key", `{"key":"FAKE-SECOND-9z8y"}`); rec.Code != 200 {
		t.Fatalf("замена: код %d", rec.Code)
	}
	if k, _ := s.keyStore().Get(); k != "FAKE-SECOND-9z8y" {
		t.Fatalf("после замены %q", k)
	}

	rec = do(t, h, http.MethodDelete, "/api/key", "")
	if rec.Code != 200 || keysOfMap(jsonOf(t, rec)) != "ok" || jsonOf(t, rec)["ok"] != true {
		t.Fatalf("DELETE: код %d: %s", rec.Code, rec.Body.String())
	}
	if got := getSettings(t, h); got.Key.Set || got.Key.Hint != "" {
		t.Fatalf("после удаления: %+v", got.Key)
	}
	if _, err := os.Stat(s.Paths.Keys); !os.IsNotExist(err) {
		t.Fatalf("keys.json остался: %v", err)
	}
	// Повторное удаление — не ошибка.
	if rec := do(t, h, http.MethodDelete, "/api/key", ""); rec.Code != 200 {
		t.Fatalf("повторное DELETE: код %d", rec.Code)
	}
}

// Прежние маршруты по профилю убраны.
func TestOldKeyRoutesAreGone(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/api/keys/deepseek"}, {http.MethodDelete, "/api/keys/deepseek"},
		{http.MethodPost, "/api/keys/deepseek/check"},
	} {
		if rec := do(t, h, c.method, c.path, `{"key":"x"}`); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: код %d", c.method, c.path, rec.Code)
		}
	}
}

func TestPutKeyRejectsMalformedKeys(t *testing.T) {
	huge := `{"key":"` + strings.Repeat("k", maxKeyBody) + `"}`
	rows := []struct {
		name, body string
		code       int
	}{
		{"пустой", `{"key":""}`, 400},
		{"из пробелов", `{"key":"   "}`, 400},
		{"ключа нет в теле", `{}`, 400},
		{"пробел внутри", `{"key":"FAKE-AAA BBB"}`, 400},
		{"перевод строки", `{"key":"FAKE-AAA\nBBB"}`, 400},
		{"NUL", `{"key":"FAKE-AAA\u0000BBB"}`, 400},
		{"4097 знаков", `{"key":"` + strings.Repeat("k", 4097) + `"}`, 400},
		{"не строка", `{"key":42}`, 400},
		{"лишнее поле", `{"key":"FAKE-AAA","profile":"x"}`, 400},
		{"не JSON", `FAKE-AAA`, 400},
		{"лишнее после", `{"key":"FAKE-AAA"} {"key":"FAKE-BBB"}`, 400},
		{"ключ в имени поля", `{"FAKE-AAA-BBB":1}`, 400},
		{"слишком большое тело", huge, 413},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			s := newTestServer(t, nil, nil)
			before, _ := os.ReadFile(s.Paths.Keys)
			rec := do(t, s.Handler(), http.MethodPut, "/api/key", r.body)
			msg := rec.Body.String()
			if rec.Code != r.code || !cyrillicRe.MatchString(msg) {
				t.Fatalf("код %d: %.200s", rec.Code, msg)
			}
			if strings.Contains(msg, "FAKE-AAA") || strings.Contains(msg, "kkkk") {
				t.Fatalf("ответ повторяет ключ: %.200s", msg)
			}
			if after, _ := os.ReadFile(s.Paths.Keys); !bytes.Equal(before, after) {
				t.Fatalf("keys.json изменился при отказе: %s", after)
			}
		})
	}
}

// (a) Ключ сквозь весь путь: сохранили, прочитали состояние и настройки,
// проверили у «провайдера», который эхом возвращает заголовок, отвергли
// кривую вставку, удалили. Ни в одном ответе, заголовке и строке журнала сервера
// нет ни ключа, ни его первых и последних пяти знаков (кроме подсказки из четырёх).
func TestKeyFullFlowLeaksNothing(t *testing.T) {
	const secret = "FAKE-LEAKFLOW-qZ8r-3mNv-ab12"
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var gotAuth atomic.Value
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		// провайдер, который отвечает 401 и эхом возвращает заголовок в теле и в заголовке ответа
		w.Header().Set("X-Echo", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("плохой ключ: " + r.Header.Get("Authorization")))
	}))
	defer fake.Close()

	s := newTestServer(t, nil, nil)
	noKey(t, s)
	writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1000}`, fake.URL))
	h := s.Handler()

	var all strings.Builder
	record := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		all.WriteString(rec.Body.String() + "\n")
		for k, v := range rec.Result().Header {
			all.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
		return rec
	}
	if rec := record(do(t, h, http.MethodPut, "/api/key", `{"key":"`+secret+`"}`)); rec.Code != 200 {
		t.Fatalf("PUT: %d", rec.Code)
	}
	record(do(t, h, http.MethodGet, "/api/state", ""))
	record(do(t, h, http.MethodGet, "/api/settings", ""))
	chk := record(do(t, h, http.MethodPost, "/api/key/check", ""))
	if m := jsonOf(t, chk); m["ok"] != false || m["status"] != 401.0 {
		t.Fatalf("проверка: %v", m)
	}
	if gotAuth.Load() != "Bearer "+secret {
		t.Fatal("провайдер получил не тот заголовок: проверка не дошла до него")
	}
	record(do(t, h, http.MethodPut, "/api/key", `{"key":"`+secret+` с пробелом"}`))
	record(do(t, h, http.MethodPut, "/api/key", `{"`+secret+`":1}`))
	record(do(t, h, http.MethodPut, "/api/key", `{"key":"`+secret+`","`+secret+`":1}`))
	record(do(t, h, http.MethodPut, "/api/key", `{"key":"`+secret))
	record(do(t, h, http.MethodPut, "/api/settings", `{"base_url":"`+secret+`","model":"m","context_budget":1}`))
	record(do(t, h, http.MethodPut, "/api/settings", `{"base_url":"https://a.example/v1","model":"`+secret+`\nx","context_budget":1}`))
	record(do(t, h, http.MethodDelete, "/api/key", ""))
	record(do(t, h, http.MethodGet, "/api/settings", ""))

	all.WriteString(logs.String())
	r := []rune(secret)
	fragments := map[string]string{
		"ключ целиком":    secret,
		"первые 5 знаков": string(r[:5]),
		"последние 5":     string(r[len(r)-5:]),
		"середина":        string(r[8 : len(r)-8]),
	}
	for name, frag := range fragments {
		if strings.Contains(all.String(), frag) {
			t.Errorf("%s (%q) просочились:\n%s", name, frag, all.String())
		}
	}
	// Подсказка из четырёх знаков в ответах была — это разрешено и ожидаемо.
	if !strings.Contains(all.String(), "…ab12") {
		t.Error("подсказка из четырёх знаков в ответах не появилась")
	}
}

// (b) Записи ключа идут под одним замком: 24 горутины пишут и удаляют подряд, файл в
// конце цел, временных файлов не осталось. Запускается под -race.
func TestKeysConcurrentWritesAndDeletes(t *testing.T) {
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	h := s.Handler()
	const n = 24
	var wg sync.WaitGroup
	sent := map[string]bool{}
	for i := 0; i < n; i++ {
		i := i
		key := fmt.Sprintf("FAKE-CONC-%02d-key", i)
		sent[key] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rec *httptest.ResponseRecorder
			if i%4 == 3 {
				rec = do(t, h, http.MethodDelete, "/api/key", "")
			} else {
				rec = do(t, h, http.MethodPut, "/api/key", `{"key":"`+key+`"}`)
			}
			if rec.Code != 200 {
				t.Errorf("запрос %d: код %d: %s", i, rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	k, err := s.keyStore().Get()
	if err != nil {
		t.Fatalf("файл ключа испорчен: %v", err)
	}
	if k != "" && !sent[k] {
		t.Fatalf("в файле чужой ключ %q", k)
	}
	entries, _ := os.ReadDir(filepath.Dir(s.Paths.Keys))
	for _, e := range entries {
		if e.Name() != "keys.json" && !strings.HasPrefix(e.Name(), "history") {
			t.Errorf("лишний файл рядом с keys.json: %s", e.Name())
		}
	}
	// Состояние читается и после гонки.
	if rec := do(t, h, http.MethodGet, "/api/state", ""); rec.Code != 200 {
		t.Fatalf("state: %d", rec.Code)
	}
}

// ---- (c) испорченный keys.json ----

const corruptKeys = `{"key": "CORRUPT-FAKE-KEY-7777", ТУТ ПОЛОМКА`

func corruptKeysServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	if err := os.WriteFile(s.Paths.Keys, []byte(corruptKeys), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCorruptKeysFileIsNeverOverwritten(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := corruptKeysServer(t)
	h := s.Handler()

	// Состояние открывается: предупреждение вместо ключа, без пути и без разбора.
	for _, path := range []string{"/api/state", "/api/settings"} {
		rec := do(t, h, http.MethodGet, path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: код %d: %s", path, rec.Code, rec.Body.String())
		}
		noPathNoParser(t, path, rec.Body.String(), s)
		key := jsonOf(t, rec)["key"].(map[string]any)
		if key["set"] != false || key["warning"] != "keys.json не читается" || key["hint"] != nil {
			t.Fatalf("%s: ключ %v", path, key)
		}
		if strings.Contains(rec.Body.String(), "CORRUPT") {
			t.Fatalf("%s: содержимое файла в ответе", path)
		}
	}
	// Запись и удаление отказывают, файл не трогают.
	for _, c := range []struct{ method, body string }{
		{http.MethodPut, `{"key":"NEW-FAKE-KEY-0000"}`}, {http.MethodDelete, ""},
	} {
		rec := do(t, h, c.method, "/api/key", c.body)
		msg := rec.Body.String()
		if rec.Code != http.StatusConflict || !strings.Contains(msg, "keys.json не читается") || !cyrillicRe.MatchString(msg) {
			t.Fatalf("%s: код %d: %s", c.method, rec.Code, msg)
		}
		noPathNoParser(t, c.method, msg, s)
		if strings.Contains(msg, "NEW-FAKE") || strings.Contains(msg, "CORRUPT") {
			t.Fatalf("%s: в ответе есть ключ: %s", c.method, msg)
		}
		if data, _ := os.ReadFile(s.Paths.Keys); string(data) != corruptKeys {
			t.Fatalf("%s: файл изменён: %q", c.method, data)
		}
	}
	// Проверка отвечает отказом, не 500 и не путём.
	rec := do(t, h, http.MethodPost, "/api/key/check", "")
	m := jsonOf(t, rec)
	if rec.Code != 200 || m["ok"] != false || m["message"] != "keys.json не читается" {
		t.Fatalf("проверка: код %d: %s", rec.Code, rec.Body.String())
	}
	if entries, _ := os.ReadDir(filepath.Dir(s.Paths.Keys)); len(entries) != 1 {
		t.Fatalf("рядом с keys.json появились файлы: %v", entries)
	}
	// В журнале сервера путь есть (там он нужен), содержимого файла нет.
	if strings.Contains(logs.String(), "CORRUPT") || strings.Contains(logs.String(), "ПОЛОМКА") {
		t.Fatalf("содержимое keys.json в журнале сервера:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), s.Paths.Keys) {
		t.Fatalf("в журнале сервера нет пути к keys.json:\n%s", logs.String())
	}
}

// ---- проверка ключа: POST /api/key/check ----

type checkResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func postCheck(t *testing.T, h http.Handler) checkResult {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/key/check", "")
	if rec.Code != 200 {
		t.Fatalf("check: код %d: %s", rec.Code, rec.Body.String())
	}
	var r checkResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// (i) Проверка идёт по адресу из настроек, шлёт только Bearer с сохранённым
// ключом, и ответ — ровно {ok, status, message}.
func TestCheckKeyUsesConfiguredAddressAndBearerOnly(t *testing.T) {
	var mu sync.Mutex
	var seen *http.Request
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = r
		mu.Unlock()
		if r.URL.Path != "/custom/v1/models" || r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer fake.Close()
	var trapHit atomic.Bool
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { trapHit.Store(true) }))
	defer trap.Close()

	s := newTestServer(t, nil, nil)
	writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1000}`, fake.URL+"/custom/v1/"))
	h := s.Handler()
	rec := do(t, h, http.MethodPost, "/api/key/check", "")
	m := jsonOf(t, rec)
	if rec.Code != 200 || m["ok"] != true || m["status"] != 200.0 || m["message"] != "ключ принят" || keysOfMap(m) != "message,ok,status" {
		t.Fatalf("ответ: код %d: %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	req := seen
	mu.Unlock()
	for name := range req.Header {
		switch http.CanonicalHeaderKey(name) {
		case "Authorization", "User-Agent", "Accept-Encoding":
		default:
			t.Errorf("лишний заголовок %s", name)
		}
	}
	if trapHit.Load() {
		t.Fatal("запрос ушёл не на настроенный адрес")
	}
}

func TestCheckKeyStatuses(t *testing.T) {
	for name, c := range map[string]struct {
		code    int
		message string
	}{
		"401": {401, "ключ отклонён: 401 Unauthorized"},
		"403": {403, "ключ отклонён: 403 Forbidden"},
		"429": {429, "провайдер ответил 429 Too Many Requests: ключ не проверен"},
		"500": {500, "провайдер ответил 500 Internal Server Error: ключ не проверен"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.code)
				w.Write([]byte("ТЕЛО-ОТВЕТА-ПРОВАЙДЕРА " + r.Header.Get("Authorization")))
			}))
			defer fake.Close()
			s := newTestServer(t, nil, nil)
			writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1}`, fake.URL))
			rec := do(t, s.Handler(), http.MethodPost, "/api/key/check", "")
			var r checkResult
			json.Unmarshal(rec.Body.Bytes(), &r)
			if r.OK || r.Status != c.code || r.Message != c.message {
				t.Fatalf("%+v", r)
			}
			if strings.Contains(rec.Body.String(), "ТЕЛО-ОТВЕТА") || strings.Contains(rec.Body.String(), testKey) {
				t.Fatalf("ответ провайдера или ключ дошёл до интерфейса: %s", rec.Body.String())
			}
		})
	}
}

// Без ключа — 200 {ok:false, message: текст про отсутствие ключа}; провайдер не
// тронут. Ключи окружения роли не играют.
func TestCheckKeyWithoutKey(t *testing.T) {
	var called atomic.Bool
	fake := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer fake.Close()
	for _, name := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(name, "FAKE-ENV-KEY-1234")
	}
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1}`, fake.URL))
	rec := do(t, s.Handler(), http.MethodPost, "/api/key/check", "")
	m := jsonOf(t, rec)
	if rec.Code != 200 || m["ok"] != false || m["message"] != app.ErrNoKey.Error() || keysOfMap(m) != "message,ok" {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if called.Load() {
		t.Fatal("запрос ушёл к провайдеру без ключа")
	}
}

// (i) Перенаправление не выполняется; недоступный адрес и молчащий провайдер дают
// короткое сообщение по-русски без адреса и ключа.
func TestCheckKeyFailures(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	var leaked atomic.Value // string: сервер пишет из своей горутины
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
	}))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer redirect.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	for name, url := range map[string]string{"порт закрыт": deadURL, "перенаправление": redirect.URL, "молчит": slow.URL} {
		s := newTestServer(t, nil, nil)
		s.keyCheckTimeout = 100 * time.Millisecond
		writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1}`, url))
		r := postCheck(t, s.Handler())
		if r.OK || !cyrillicRe.MatchString(r.Message) {
			t.Errorf("%s: %+v", name, r)
		}
		if name == "перенаправление" && r.Status != http.StatusFound {
			t.Errorf("%s: статус %d, ожидался 302", name, r.Status)
		}
		for _, leak := range []string{testKey, url, "127.0.0.1"} {
			if strings.Contains(r.Message, leak) {
				t.Errorf("%s: в сообщении %q", name, leak)
			}
		}
	}
	if leaked.Load() != nil {
		t.Fatal("ключ ушёл по перенаправлению на чужой адрес")
	}
	if strings.Contains(logs.String(), testKey) {
		t.Fatalf("ключ в журнале сервера:\n%s", logs.String())
	}
}

// Адрес для проверки берётся при испорченном config.json из умолчаний — но
// не из чужого окружения; сеть тут не нужна: подставляем клиента-перехватчика.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCheckKeyUnderCorruptConfigUsesDefaultAddress(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, `{"model": ТУТ ПОЛОМКА`)
	var urls []string
	s.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String()+" "+r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})}
	if r := postCheck(t, s.Handler()); !r.OK {
		t.Fatalf("%+v", r)
	}
	if len(urls) != 1 || urls[0] != "https://api.deepseek.com/v1/models Bearer "+testKey {
		t.Fatalf("запросы %v", urls)
	}
}

// ---- (f) окружение ключей не читается ----

// Ключи из окружения сервер не знает: при DEEPSEEK_API_KEY и прочих в окружении
// и без keys.json ход и «ещё раз» отвечают 400 с текстом об отсутствии ключа,
// провайдер не создаётся, журнал не пишется, а в состоянии ключа нет.
func TestEnvironmentKeysAreIgnored(t *testing.T) {
	for _, name := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(name, "FAKE-ENV-KEY-9999")
	}
	for _, real := range []bool{false, true} { // подмена провайдера и настоящий конструктор
		t.Run(fmt.Sprintf("настоящий конструктор=%v", real), func(t *testing.T) {
			s := newTestServer(t, nil, nil)
			noKey(t, s)
			s.NewProvider = func(app.Connection) (provider.Provider, error) {
				t.Fatal("провайдер создан без ключа в keys.json")
				return nil, nil
			}
			if real {
				s.NewProvider = nil
			}
			// У беседы уже есть журнал: «ещё раз» должно дойти до проверки ключа.
			writeJournal(t, s.Paths.History, convID,
				chat.Record{Role: "user", Text: "прежний вопрос"}, chat.Record{Role: "assistant", Text: "прежний ответ\n"})
			journal := filepath.Join(s.Paths.History, convID+".jsonl")
			before, _ := os.ReadFile(journal)
			h := s.Handler()
			for _, path := range []string{"/turns", "/again"} {
				rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+path, `{"text":"вопрос"}`)
				if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), app.ErrNoKey.Error()) {
					t.Fatalf("%s: код %d: %s", path, rec.Code, rec.Body.String())
				}
			}
			if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
				t.Fatalf("журнал изменён отказанным ходом:\n%s", after)
			}
			key := jsonOf(t, do(t, h, http.MethodGet, "/api/state", ""))["key"].(map[string]any)
			if key["set"] != false {
				t.Fatalf("ключ из окружения попал в состояние: %v", key)
			}
		})
	}
}

// (f) Ход без ключа: 400 с точным текстом, журнал не тронут (в том числе у беседы,
// где уже есть записи, и «ещё раз» вопрос не отзывает).
func TestTurnWithoutKeyIs400AndWritesNothing(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, fakeFinder{})
	h := s.Handler()
	if rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`); rec.Code != 200 {
		t.Fatalf("первый ход: %d", rec.Code)
	}
	journal := filepath.Join(s.Paths.History, convID+".jsonl")
	before, _ := os.ReadFile(journal)
	noKey(t, s)
	for _, path := range []string{"/turns", "/again"} {
		rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+path, `{"text":"второй"}`)
		if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != app.ErrNoKey.Error() {
			t.Fatalf("%s: код %d: %q", path, rec.Code, rec.Body.String())
		}
	}
	if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
		t.Fatalf("журнал изменился:\n%s", after)
	}
	if recs, _ := (chat.History{Path: journal}).Load(); len(recs) != 2 {
		t.Fatalf("записей %d", len(recs))
	}
}

// Ход берёт подключение из сохранённых настроек и ключа; лишнее поле profile в теле
// игнорируется.
func TestTurnUsesSavedSettingsAndIgnoresProfileField(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, `{"base_url":"https://gw.example/v1","model":"vendor/model-x","context_budget":777}`)
	var got app.Connection
	s.NewProvider = func(c app.Connection) (provider.Provider, error) {
		got = c
		return &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, nil
	}
	s.Finder = fakeFinder{}
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос","profile":"claude"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("код %d: %.300s", rec.Code, rec.Body.String())
	}
	want := app.Connection{BaseURL: "https://gw.example/v1", Model: "vendor/model-x", Key: testKey, ContextBudget: 777}
	if got != want {
		t.Fatalf("подключение %+v, ожидалось %+v", got, want)
	}
	if b := s.conv(convID).session.Budget; b != 777 {
		t.Fatalf("бюджет сессии %d", b)
	}
	// В журнале — модель, но ни профиля, ни цены.
	data, _ := os.ReadFile(filepath.Join(s.Paths.History, convID+".jsonl"))
	for _, gone := range []string{`"profile"`, `"cost_usd"`, `"cache_write"`} {
		if strings.Contains(string(data), gone) {
			t.Errorf("в новом журнале есть %s:\n%s", gone, data)
		}
	}
	if !strings.Contains(string(data), `"model":"vendor/model-x"`) {
		t.Fatalf("в журнале нет модели:\n%s", data)
	}
	// SSE usage без цен.
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "usage" {
			if got := keysOfMap(ev.Data); got != "context_budget,context_used,conversation_tokens,usage" {
				t.Fatalf("поля события usage: %s", got)
			}
			u := ev.Data["usage"].(map[string]any)
			if _, has := u["cache_write"]; has {
				t.Errorf("в usage есть cache_write: %v", u)
			}
		}
	}
}

// Ни ключа, ни тела ответа провайдера (оно может повторять ключ) интерфейсу не
// передаётся целиком: ошибка хода доходит, но уже без ключа.
func TestKeyFromTurnErrorIsNotLeaked(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.NewProvider = nil
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"Incorrect API key provided: ` + r.Header.Get("Authorization")[len("Bearer "):] + `"}`))
	}))
	defer fake.Close()
	writeConfig(t, s, fmt.Sprintf(`{"base_url":%q,"model":"m","context_budget":1000}`, fake.URL))
	s.Finder = fakeFinder{}
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	data, _ := os.ReadFile(filepath.Join(s.Paths.History, convID+".jsonl"))
	if !strings.Contains(rec.Body.String(), "event: error") || !strings.Contains(rec.Body.String(), "401") {
		t.Fatalf("ошибка хода не дошла: %.300s", rec.Body.String())
	}
	for _, where := range []string{rec.Body.String(), string(data), logs.String()} {
		if strings.Contains(where, testKey) {
			t.Fatalf("ключ просочился:\n%.400s", where)
		}
	}
}

var _ = keys.UnreadableText
