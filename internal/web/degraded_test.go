package web

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
)

// Испорченный config.json не должен гасить интерфейс: сервер переходит на
// умолчания, говорит об этом полями degraded и warning и ни в одном ответе не
// называет путь к файлу и не цитирует разбор. Config.json старого формата (с
// профилями) для сервера — такой же испорченный: ровно тот же путь.

const (
	configWarning = "config.json не читается — исправьте файл или сохраните настройки заново (будет перезаписан)"
	corruptConfig = `{"model": "x", ТУТ ПОЛОМКА`
	// oldFormatConfig — config.json версии с профилями: валидный JSON, но
	// неизвестные нынешней версии поля.
	oldFormatConfig = `{"default": "deepseek-flash", "corpus_k": 8, "profiles": {"deepseek": {"model": "deepseek-v4-pro", "prices": {"input": 1, "output": 2}, "reasoning_effort": "high"}}}`
)

// brokenConfigs — всё, что для сервера «config.json не читается».
var brokenConfigs = map[string]string{
	"не JSON":        corruptConfig,
	"старый формат":  oldFormatConfig,
	"пустой файл":    ``,
	"поле из буд.":   `{"model":"m","thinking":"max"}`,
	"недопустимое":   `{"base_url":"ftp://x/v1"}`,
	"бюджет строкой": `{"context_budget":"много"}`,
}

func brokenConfigServer(t *testing.T, body string) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	if err := os.MkdirAll(filepath.Dir(s.Paths.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Paths.Config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

func corruptConfigServer(t *testing.T) *Server { return brokenConfigServer(t, corruptConfig) }

// noPathNoParser — в теле нет ни пути к файлам пользователя, ни текста разбора.
func noPathNoParser(t *testing.T, label, body string, s *Server) {
	t.Helper()
	for _, leak := range []string{s.Paths.Config, s.Paths.Keys, filepath.Dir(s.Paths.Config), "invalid character", "looking for",
		"ПОЛОМКА", "ТУТ", "unknown field", "json:", "CORRUPT"} {
		if strings.Contains(body, leak) {
			t.Errorf("%s: в ответе есть %q", label, leak)
		}
	}
}

type degradedDoc struct {
	BaseURL       string  `json:"base_url"`
	Model         string  `json:"model"`
	ContextBudget int     `json:"context_budget"`
	Degraded      bool    `json:"degraded"`
	Warning       string  `json:"warning"`
	Key           keyView `json:"key"`
}

// (e) Любой нечитаемый config.json — и старый формат тоже: /api/state и
// /api/settings отвечают 200 с умолчаниями, degraded и ровно этим предупреждением.
func TestBrokenConfigStateAndSettingsFallBackToDefaults(t *testing.T) {
	for name, body := range brokenConfigs {
		t.Run(name, func(t *testing.T) {
			s := brokenConfigServer(t, body)
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
				if !d.Degraded || d.Warning != configWarning {
					t.Errorf("%s: degraded=%v, warning=%q", path, d.Degraded, d.Warning)
				}
				if d.BaseURL != config.DefaultBaseURL || d.Model != config.DefaultModel || d.ContextBudget != config.DefaultContextBudget {
					t.Errorf("%s: не умолчания: %+v", path, d)
				}
				// Ключ при этом читается как обычно.
				if !d.Key.Set || d.Key.Hint != "…4f1d" {
					t.Errorf("%s: ключ в режиме без конфига: %+v", path, d.Key)
				}
			}
		})
	}
}

// Здоровый конфиг — ни degraded, ни warning в ответе нет вовсе.
func TestHealthyConfigHasNoDegradedKeys(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, `{"model":"особая"}`)
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
		if m["model"] != "особая" || m["base_url"] != config.DefaultBaseURL || m["context_budget"] != float64(config.DefaultContextBudget) {
			t.Errorf("%s: поверх умолчаний должна лечь только модель: %v", path, m)
		}
	}
}

// Диалог живёт: ход проходит на умолчаниях и пишется в журнал, как при здоровом
// конфиге.
func TestTurnWorksUnderBrokenConfig(t *testing.T) {
	for name, body := range map[string]string{"не JSON": corruptConfig, "старый формат": oldFormatConfig} {
		t.Run(name, func(t *testing.T) {
			s := brokenConfigServer(t, body)
			var got app.Connection
			s.NewProvider = func(c app.Connection) (provider.Provider, error) {
				got = c
				return &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, nil
			}
			s.Finder = fakeFinder{}
			rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "event: done") {
				t.Fatalf("код %d: %.300s", rec.Code, rec.Body.String())
			}
			want := app.Connection{BaseURL: config.DefaultBaseURL, Model: config.DefaultModel, Key: testKey, ContextBudget: config.DefaultContextBudget}
			if got != want {
				t.Fatalf("подключение %+v, ожидалось умолчание %+v", got, want)
			}
			recs, _ := chat.History{Path: filepath.Join(s.Paths.History, convID+".jsonl")}.Load()
			if len(recs) != 2 || recs[0].Role != "user" || recs[1].Role != "assistant" || recs[1].Model != config.DefaultModel {
				t.Fatalf("журнал %+v", recs)
			}
			noPathNoParser(t, "ход", rec.Body.String(), s)
		})
	}
}

func TestTurnWithoutKeyUnderCorruptConfigHidesPath(t *testing.T) {
	s := corruptConfigServer(t)
	noKey(t, s)
	s.NewProvider = nil // настоящий конструктор
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != app.ErrNoKey.Error() {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	noPathNoParser(t, "ход без ключа", rec.Body.String(), s)
}

// Ключ правится и при испорченном конфиге.
func TestKeyHandlersWorkUnderCorruptConfig(t *testing.T) {
	s := corruptConfigServer(t)
	h := s.Handler()
	if rec := do(t, h, http.MethodPut, "/api/key", `{"key":"FAKE-DEGRADED-KEY-1234"}`); rec.Code != 200 {
		t.Fatalf("PUT: код %d: %s", rec.Code, rec.Body.String())
	}
	if k, _ := s.keyStore().Get(); k != "FAKE-DEGRADED-KEY-1234" {
		t.Fatalf("ключ %q", k)
	}
	if rec := do(t, h, http.MethodDelete, "/api/key", ""); rec.Code != 200 {
		t.Fatalf("DELETE: код %d", rec.Code)
	}
	if data, _ := os.ReadFile(s.Paths.Config); string(data) != corruptConfig {
		t.Fatalf("правка ключа тронула config.json: %q", data)
	}
}

func fixedClock(s *Server) string {
	s.clock = func() time.Time { return time.Date(2026, 11, 18, 12, 34, 56, 0, time.UTC) }
	return "config.json.bad-20261118T123456Z"
}

// Сохранение поверх испорченного файла разрешено, но файл руками правленный (или
// оставшийся от версии с профилями): сначала копия рядом (0600, исходные
// байты), потом запись в новом формате; ответ называет только имя копии.
// Повторное сохранение копию не трогает и новой не делает.
func TestPutSettingsOverBrokenConfigBacksItUpFirst(t *testing.T) {
	for name, original := range map[string]string{"не JSON": corruptConfig, "старый формат": oldFormatConfig} {
		t.Run(name, func(t *testing.T) {
			s := brokenConfigServer(t, original)
			want := fixedClock(s)
			h := s.Handler()
			if doc := getSettings(t, h); !doc.Degraded {
				t.Fatalf("GET не сообщил о повреждении: %+v", doc)
			}
			rec := putSettings(t, h, `{"base_url":"https://openrouter.ai/api/v1","model":"anthropic/claude-opus-5","context_budget":32000}`)
			if rec.Code != 200 {
				t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
			}
			noPathNoParser(t, "PUT", rec.Body.String(), s)
			var out settingsDoc
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Backup != want || out.Degraded || out.Warning != "" || out.Model != "anthropic/claude-opus-5" {
				t.Fatalf("backup %q, degraded %v, warning %q, model %q", out.Backup, out.Degraded, out.Warning, out.Model)
			}
			cfg, err := config.Load(s.Paths.Config)
			if err != nil || cfg != (config.Config{BaseURL: "https://openrouter.ai/api/v1", Model: "anthropic/claude-opus-5", ContextBudget: 32000}) {
				t.Fatalf("конфиг после сохранения: %+v, %v", cfg, err)
			}
			backup := filepath.Join(filepath.Dir(s.Paths.Config), out.Backup)
			data, err := os.ReadFile(backup)
			if err != nil || string(data) != original {
				t.Fatalf("копия %q, %v", data, err)
			}
			if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o600 {
				t.Errorf("права копии %v", fi.Mode().Perm())
			}
			// второй PUT: файл уже в порядке — копия не нужна и не тронута
			before, _ := os.Stat(backup)
			rec2 := putSettings(t, h, `{"base_url":"https://openrouter.ai/api/v1","model":"anthropic/claude-opus-5","context_budget":16000}`)
			if rec2.Code != 200 || strings.Contains(rec2.Body.String(), `"backup"`) {
				t.Fatalf("второй PUT: код %d, %s", rec2.Code, rec2.Body.String())
			}
			after, _ := os.Stat(backup)
			if !after.ModTime().Equal(before.ModTime()) {
				t.Error("копия тронута вторым сохранением")
			}
			if m, _ := filepath.Glob(s.Paths.Config + ".bad-*"); len(m) != 1 {
				t.Errorf("копий %v", m)
			}
		})
	}
}

// validSettings — допустимый документ настроек для PUT.
const validSettings = `{"base_url":"https://api.example.com/v1","model":"m","context_budget":1000}`

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
		rec := putSettings(t, h, validSettings)
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
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })
		rec := putSettings(t, h, validSettings)
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
	if rec := putSettings(t, h, `{"base_url":"ftp://x","model":"m","context_budget":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d", rec.Code)
	}
	if m, _ := filepath.Glob(s.Paths.Config + ".bad-*"); len(m) != 0 {
		t.Errorf("копия при отказе по документу: %v", m)
	}
}

// Оба файла испорчены: состояние открывается, у ключа — предупреждение о
// keys.json, у ответа — о конфиге; два предупреждения, не одно общее: чинятся
// они разными руками. Сохранение чинит config.json и не трогает keys.json.
func TestBothFilesCorrupt(t *testing.T) {
	s := corruptKeysServer(t)
	if err := os.WriteFile(s.Paths.Config, []byte(oldFormatConfig), 0o600); err != nil {
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
	if !d.Degraded || d.Warning != configWarning || d.Key != (keyView{Warning: "keys.json не читается"}) {
		t.Fatalf("%+v", d)
	}
	put := putSettings(t, h, validSettings)
	if put.Code != 200 || !strings.Contains(put.Body.String(), `"backup":"config.json.bad-`) {
		t.Fatalf("PUT: код %d: %s", put.Code, put.Body.String())
	}
	noPathNoParser(t, "PUT", put.Body.String(), s)
	if keysAfter, _ := os.ReadFile(s.Paths.Keys); !bytes.Equal(keysBefore, keysAfter) {
		t.Fatal("keys.json тронут")
	}
	var after degradedDoc
	json.Unmarshal(do(t, h, http.MethodGet, "/api/settings", "").Body.Bytes(), &after)
	if after.Degraded || after.Warning != "" || after.Key.Warning != "keys.json не читается" {
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
