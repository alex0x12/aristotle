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
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/provider"
)

// Испорченный config.json не должен гасить интерфейс: сервер переходит на
// встроенные профили, говорит об этом полями degraded и warning и ни в одном
// ответе не называет путь к файлу и не цитирует разбор.

const (
	configWarning = "config.json не читается — исправьте файл или сохраните настройки заново (будет перезаписан)"
	corruptConfig = `{"default": "deepseek", "profiles": {"claude": ТУТ ПОЛОМКА`
)

func corruptConfigServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	if err := os.MkdirAll(filepath.Dir(s.Paths.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Paths.Config, []byte(corruptConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

// noPathNoParser — в теле нет ни пути к файлам пользователя, ни текста разбора.
func noPathNoParser(t *testing.T, label, body string, s *Server) {
	t.Helper()
	for _, leak := range []string{s.Paths.Config, s.Paths.Keys, filepath.Dir(s.Paths.Config), "invalid character", "looking for", "ПОЛОМКА", "ТУТ"} {
		if strings.Contains(body, leak) {
			t.Errorf("%s: в ответе есть %q", label, leak)
		}
	}
}

var builtinNames = []string{"claude", "deepseek", "deepseek-flash", "openai", "openrouter"}

type degradedDoc struct {
	Default  string `json:"default"`
	CorpusK  int    `json:"corpus_k"`
	Degraded bool   `json:"degraded"`
	Warning  string `json:"warning"`
	Profiles []struct {
		Name    string  `json:"name"`
		Builtin bool    `json:"builtin"`
		Key     keyView `json:"key"`
	} `json:"profiles"`
}

func TestCorruptConfigStateAndSettingsFallBackToBuiltin(t *testing.T) {
	s := corruptConfigServer(t)
	t.Setenv("DEEPSEEK_API_KEY", "zq-из-окружения-7070") // ключи окружения работают и так
	for _, path := range []string{"/api/state", "/api/settings"} {
		rec := do(t, s.Handler(), http.MethodGet, path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: код %d: %s", path, rec.Code, rec.Body.String())
		}
		noPathNoParser(t, path, rec.Body.String(), s)
		var d degradedDoc
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range d.Profiles {
			names = append(names, p.Name)
			if p.Name == "deepseek" && p.Key != (keyView{Source: "env", Hint: "…7070"}) {
				t.Errorf("%s: ключ из окружения в режиме без конфига: %+v", path, p.Key)
			}
		}
		sort.Strings(names)
		if strings.Join(names, ",") != strings.Join(builtinNames, ",") {
			t.Errorf("%s: профили %v", path, names)
		}
		if !d.Degraded || d.Warning != configWarning || d.Default != "deepseek" || d.CorpusK != 6 {
			t.Errorf("%s: degraded=%v, warning=%q, default=%q, corpus_k=%d", path, d.Degraded, d.Warning, d.Default, d.CorpusK)
		}
	}
}

// Здоровый конфиг — ни degraded, ни warning в ответе нет вовсе.
func TestHealthyConfigHasNoDegradedKeys(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, `{"profiles":{"claude":{"model":"особая"}}}`)
	for _, path := range []string{"/api/state", "/api/settings"} {
		var m map[string]any
		if err := json.Unmarshal(do(t, s.Handler(), http.MethodGet, path, "").Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"degraded", "warning", "backup"} {
			if _, ok := m[k]; ok {
				t.Errorf("%s: в ответе здорового конфига есть %q", path, k)
			}
		}
	}
}

// Диалог живёт: ход с ключом из окружения проходит и пишется в журнал, как при
// здоровом конфиге; без ключа — отказ без пути.
func TestTurnWorksUnderCorruptConfig(t *testing.T) {
	s := corruptConfigServer(t)
	t.Setenv("DEEPSEEK_API_KEY", "zq-из-окружения-7070")
	s.NewProvider = func(config.Profile) (provider.Provider, error) {
		return &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, nil
	}
	s.Finder = fakeFinder{}
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("код %d: %.300s", rec.Code, rec.Body.String())
	}
	recs, _ := chat.History{Path: filepath.Join(s.Paths.History, convID+".jsonl")}.Load()
	if len(recs) != 2 || recs[0].Role != "user" || recs[1].Role != "assistant" || recs[1].Profile != "deepseek" {
		t.Fatalf("журнал %+v", recs)
	}
	noPathNoParser(t, "ход", rec.Body.String(), s)
}

func TestTurnWithoutKeyUnderCorruptConfigHidesPath(t *testing.T) {
	s := corruptConfigServer(t)
	s.NewProvider = nil // настоящий конструктор: без ключа он отказывает
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "DEEPSEEK_API_KEY") {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	noPathNoParser(t, "ход без ключа", rec.Body.String(), s)
}

// Ключи встроенных профилей правятся и при испорченном конфиге.
func TestKeyHandlersWorkUnderCorruptConfig(t *testing.T) {
	s := corruptConfigServer(t)
	h := s.Handler()
	if rec := do(t, h, http.MethodPut, "/api/keys/claude", `{"key":"sk-ant-для-теста"}`); rec.Code != 200 {
		t.Fatalf("PUT: код %d: %s", rec.Code, rec.Body.String())
	}
	if k, _ := (keys.Store{Path: s.Paths.Keys}).Get("claude"); k != "sk-ant-для-теста" {
		t.Fatalf("ключ %q", k)
	}
	if rec := do(t, h, http.MethodPut, "/api/keys/свой", `{"key":"x1234"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("неизвестный профиль: код %d", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/api/keys/claude", ""); rec.Code != 200 {
		t.Fatalf("DELETE: код %d", rec.Code)
	}
}

func fixedClock(s *Server) string {
	s.clock = func() time.Time { return time.Date(2026, 11, 18, 12, 34, 56, 0, time.UTC) }
	return "config.json.bad-20261118T123456Z"
}

// Сохранение поверх испорченного файла разрешено, но файл руками правленный:
// сначала копия рядом (0600, исходные байты), потом запись; ответ называет только
// имя копии. Повторное сохранение копию не трогает и новой не делает.
func TestPutSettingsOverCorruptConfigBacksItUpFirst(t *testing.T) {
	s := corruptConfigServer(t)
	want := fixedClock(s)
	h := s.Handler()
	doc := getSettings(t, h)
	if !doc.Degraded {
		t.Fatalf("GET не сообщил о повреждении: %+v", doc)
	}
	profileOf(t, &doc, "deepseek").ContextBudget = 32000 // документ с degraded/warning принимается как есть
	rec := putSettings(t, h, doc)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	noPathNoParser(t, "PUT", rec.Body.String(), s)
	var out settingsDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Backup != want || out.Degraded || out.Warning != "" {
		t.Fatalf("backup %q, degraded %v, warning %q", out.Backup, out.Degraded, out.Warning)
	}
	cfg, err := config.Load(s.Paths.Config)
	if err != nil || cfg.Profiles["deepseek"].ContextBudget != 32000 {
		t.Fatalf("конфиг после сохранения: %+v, %v", cfg, err)
	}
	backup := filepath.Join(filepath.Dir(s.Paths.Config), out.Backup)
	data, err := os.ReadFile(backup)
	if err != nil || string(data) != corruptConfig {
		t.Fatalf("копия %q, %v", data, err)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o600 {
		t.Errorf("права копии %v", fi.Mode().Perm())
	}
	// второй PUT: файл уже в порядке — копия не нужна и не тронута
	before, _ := os.Stat(backup)
	var out2 settingsDoc
	rec2 := putSettings(t, h, out)
	json.Unmarshal(rec2.Body.Bytes(), &out2)
	if rec2.Code != 200 || out2.Backup != "" || strings.Contains(rec2.Body.String(), `"backup"`) {
		t.Fatalf("второй PUT: код %d, %s", rec2.Code, rec2.Body.String())
	}
	after, _ := os.Stat(backup)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("копия тронута вторым сохранением")
	}
	if m, _ := filepath.Glob(s.Paths.Config + ".bad-*"); len(m) != 1 {
		t.Errorf("копий %v", m)
	}
}

// Копию сделать не удалось — отказ по-русски, испорченный файл не тронут.
func TestPutSettingsRefusesWhenBackupCannotBeMade(t *testing.T) {
	t.Run("имя копии занято", func(t *testing.T) {
		s := corruptConfigServer(t)
		name := fixedClock(s)
		other := filepath.Join(filepath.Dir(s.Paths.Config), name)
		if err := os.WriteFile(other, []byte("чужая копия"), 0o600); err != nil {
			t.Fatal(err)
		}
		h := s.Handler()
		rec := putSettings(t, h, getSettings(t, h))
		if rec.Code != http.StatusConflict || !cyrillicRe.MatchString(rec.Body.String()) {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		noPathNoParser(t, "отказ", rec.Body.String(), s)
		if data, _ := os.ReadFile(s.Paths.Config); string(data) != corruptConfig {
			t.Fatalf("испорченный файл тронут: %q", data)
		}
		if data, _ := os.ReadFile(other); string(data) != "чужая копия" {
			t.Fatalf("чужая копия перезаписана: %q", data)
		}
	})
	t.Run("каталог без записи", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root пишет в любой каталог")
		}
		s := corruptConfigServer(t)
		fixedClock(s)
		dir := filepath.Dir(s.Paths.Config)
		h := s.Handler()
		doc := getSettings(t, h)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		rec := putSettings(t, h, doc)
		if rec.Code != http.StatusInternalServerError || !cyrillicRe.MatchString(rec.Body.String()) {
			t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
		}
		noPathNoParser(t, "отказ", rec.Body.String(), s)
		if data, _ := os.ReadFile(s.Paths.Config); string(data) != corruptConfig {
			t.Fatalf("испорченный файл тронут: %q", data)
		}
	})
}

// Проверка документа идёт раньше копии: плохой документ не оставляет следов.
func TestPutSettingsValidatesBeforeBackup(t *testing.T) {
	s := corruptConfigServer(t)
	fixedClock(s)
	h := s.Handler()
	doc := getSettings(t, h)
	doc.CorpusK = 0
	if rec := putSettings(t, h, doc); rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d", rec.Code)
	}
	if m, _ := filepath.Glob(s.Paths.Config + ".bad-*"); len(m) != 0 {
		t.Errorf("копия при отказе по документу: %v", m)
	}
}

// Оба файла испорчены: состояние открывается, у профилей — предупреждения о
// ключах (как раньше), у ответа — о конфиге; два предупреждения, не одно общее:
// чинятся они разными руками. Сохранение чинит config.json и не трогает keys.json.
func TestBothFilesCorrupt(t *testing.T) {
	s := corruptKeysServer(t)
	if err := os.WriteFile(s.Paths.Config, []byte(corruptConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	fixedClock(s)
	h := s.Handler()
	keysBefore, _ := os.ReadFile(s.Paths.Keys)
	rec := do(t, h, http.MethodGet, "/api/settings", "")
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	noPathNoParser(t, "GET", rec.Body.String(), s)
	var d degradedDoc
	json.Unmarshal(rec.Body.Bytes(), &d)
	if !d.Degraded || d.Warning != configWarning || len(d.Profiles) != 5 {
		t.Fatalf("%+v", d)
	}
	for _, p := range d.Profiles {
		if p.Key != (keyView{Source: "none", Warning: "keys.json не читается"}) {
			t.Errorf("%s: %+v", p.Name, p.Key)
		}
	}
	put := putSettings(t, h, getSettings(t, h))
	if put.Code != 200 || !strings.Contains(put.Body.String(), `"backup":"config.json.bad-`) {
		t.Fatalf("PUT: код %d: %s", put.Code, put.Body.String())
	}
	noPathNoParser(t, "PUT", put.Body.String(), s)
	if keysAfter, _ := os.ReadFile(s.Paths.Keys); !bytes.Equal(keysBefore, keysAfter) {
		t.Fatal("keys.json тронут")
	}
	var after degradedDoc
	json.Unmarshal(do(t, h, http.MethodGet, "/api/settings", "").Body.Bytes(), &after)
	if after.Degraded || after.Warning != "" || after.Profiles[0].Key.Warning != "keys.json не читается" {
		t.Fatalf("после сохранения: %+v", after)
	}
}

// Страница спрашивает состояние часто: одна и та же ошибка пишется в журнал
// сервера один раз (с путём — там он нужен), а после починки и новой поломки
// — снова.
func TestConfigErrorIsLoggedOncePerBreakage(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := corruptConfigServer(t)
	h := s.Handler()
	for i := 0; i < 3; i++ {
		do(t, h, http.MethodGet, "/api/state", "")
	}
	if n := strings.Count(logs.String(), s.Paths.Config); n != 1 {
		t.Fatalf("строк о config.json %d:\n%s", n, logs.String())
	}
	writeConfig(t, s, `{}`)
	do(t, h, http.MethodGet, "/api/state", "")
	writeConfig(t, s, corruptConfig)
	do(t, h, http.MethodGet, "/api/state", "")
	if n := strings.Count(logs.String(), s.Paths.Config); n != 2 {
		t.Fatalf("после починки и новой поломки строк %d:\n%s", n, logs.String())
	}
}
