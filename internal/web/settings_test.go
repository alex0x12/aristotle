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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
)

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

func putSettings(t *testing.T, h http.Handler, doc settingsDoc) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return do(t, h, http.MethodPut, "/api/settings", string(body))
}

func profileOf(t *testing.T, doc *settingsDoc, name string) *profileView {
	t.Helper()
	for i := range doc.Profiles {
		if doc.Profiles[i].Name == name {
			return &doc.Profiles[i]
		}
	}
	t.Fatalf("в настройках нет профиля %q", name)
	return nil
}

func TestPutSettingsSavesOverrides(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	var doc settingsDoc
	json.Unmarshal(do(t, h, http.MethodGet, "/api/settings", "").Body.Bytes(), &doc)
	for i := range doc.Profiles {
		if doc.Profiles[i].Name == "deepseek" {
			doc.Profiles[i].ContextBudget = 32000
			doc.Profiles[i].Prices = &config.Prices{Input: 0.27, Output: 1.1, CacheRead: 0.07}
		}
	}
	doc.Profiles = append(doc.Profiles, profileView{Name: "local", Kind: "openai",
		BaseURL: "http://127.0.0.1:11434/v1", Model: "qwen3", ContextBudget: 16000})
	doc.CorpusK = 4
	body, _ := json.Marshal(doc)
	if rec := do(t, h, http.MethodPut, "/api/settings", string(body)); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	cfg, err := config.Load(s.Paths.Config)
	if err != nil || cfg.Profiles["deepseek"].ContextBudget != 32000 || cfg.Profiles["local"].Model != "qwen3" || cfg.CorpusK != 4 {
		t.Fatalf("конфиг %+v, %v", cfg, err)
	}
}

func TestPutSettingsValidates(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	bad := []string{
		`{"default":"deepseek","corpus_k":6,"profiles":[{"name":"Плохое имя","kind":"openai","base_url":"https://x","model":"m"}]}`,
		`{"default":"deepseek","corpus_k":6,"profiles":[{"name":"deepseek","kind":"gemini","model":"m"}]}`,
		`{"default":"deepseek","corpus_k":6,"profiles":[{"name":"deepseek","kind":"openai","base_url":"ftp://x","model":"m"}]}`,
		`{"default":"deepseek","corpus_k":6,"profiles":[{"name":"deepseek","kind":"openai","base_url":"https://x","model":"m","temperature":3}]}`,
		`{"default":"нет","corpus_k":6,"profiles":[{"name":"deepseek","kind":"openai","base_url":"https://x","model":"m"}]}`,
		`{"default":"deepseek","corpus_k":0,"profiles":[{"name":"deepseek","kind":"openai","base_url":"https://x","model":"m"}]}`,
	}
	for _, b := range bad {
		if rec := do(t, h, http.MethodPut, "/api/settings", b); rec.Code != http.StatusBadRequest {
			t.Errorf("код %d для %s", rec.Code, b)
		}
	}
}

var cyrillicRe = regexp.MustCompile(`[а-яА-ЯёЁ]`)

// Каждое нарушение — 400, сообщение по-русски и с названием поля; файл
// настроек при отказе не создаётся и не меняется.
func TestPutSettingsRejectsBadFields(t *testing.T) {
	long := strings.Repeat("a", 33)
	cases := []struct {
		name   string
		field  string // что должно быть названо в сообщении
		mutate func(d *settingsDoc, t *testing.T)
	}{
		{"глав на поиск 0", "corpus_k", func(d *settingsDoc, _ *testing.T) { d.CorpusK = 0 }},
		{"глав на поиск 21", "corpus_k", func(d *settingsDoc, _ *testing.T) { d.CorpusK = 21 }},
		{"профиля по умолчанию нет", "default", func(d *settingsDoc, _ *testing.T) { d.Default = "нет-такого" }},
		{"список профилей пуст", "profiles", func(d *settingsDoc, _ *testing.T) { d.Profiles = nil }},
		{"имя с пробелом", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = "Плохое имя" }},
		{"пустое имя", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = "" }},
		{"имя с косой чертой", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = "a/b" }},
		{"имя ../", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = "../x" }},
		{"имя из 33 знаков", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = long }},
		{"имя с заглавной", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Name = "Local" }},
		{"имя дважды", "name", func(d *settingsDoc, _ *testing.T) { d.Profiles[1].Name = d.Profiles[0].Name }},
		{"тип gemini", "kind", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Kind = "gemini" }},
		{"пустой тип", "kind", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Kind = "" }},
		{"модель пуста", "model", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Model = "  " }},
		{"модель с переводом строки", "model", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Model = "m\nx" }},
		{"адрес ftp", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "ftp://x" }},
		{"адрес javascript", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "javascript:alert(1)" }},
		{"адрес без хоста", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "https://" }},
		{"адрес без схемы", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "api.example.com/v1" }},
		{"адрес с логином", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "https://u:p@host/v1" }},
		{"адрес с пробелом", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "https://a b/v1" }},
		{"адрес пуст у openai", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").BaseURL = "" }},
		{"адрес anthropic ftp", "base_url", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "claude").BaseURL = "ftp://x" }},
		{"переменная ключа с пробелом", "key_env", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").KeyEnv = "MY KEY" }},
		{"переменная ключа с цифры", "key_env", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").KeyEnv = "1KEY" }},
		{"температура 3 у openai", "temperature", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").Temperature = f64(3) }},
		{"температура меньше нуля", "temperature", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").Temperature = f64(-0.1) }},
		{"температура 1,5 у anthropic", "temperature", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "claude").Temperature = f64(1.5) }},
		{"температура у рассуждающей", "temperature", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "openai").Temperature = f64(0.7) }},
		{"усилие неизвестное", "reasoning_effort", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "openai").ReasoningEffort = "ultra" }},
		{"усилие у обычной модели", "reasoning_effort", func(d *settingsDoc, t *testing.T) { profileOf(t, d, "deepseek").ReasoningEffort = "high" }},
		{"усилие у anthropic", "reasoning_effort", func(d *settingsDoc, t *testing.T) {
			p := profileOf(t, d, "claude")
			p.Reasoning, p.ReasoningEffort = true, "low"
		}},
		{"потолок ответа отрицательный", "max_output_tokens", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].MaxOutputTokens = -1 }},
		{"потолок ответа огромный", "max_output_tokens", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].MaxOutputTokens = 2_000_000 }},
		{"бюджет отрицательный", "context_budget", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].ContextBudget = -1 }},
		{"бюджет огромный", "context_budget", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].ContextBudget = 100_000_000 }},
		{"цена отрицательная", "prices", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Prices = &config.Prices{Output: -1} }},
		{"цена кэша отрицательная", "prices", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Prices = &config.Prices{CacheWrite: -0.01} }},
		{"цена огромная", "prices", func(d *settingsDoc, _ *testing.T) { d.Profiles[0].Prices = &config.Prices{Input: 1e300} }},
	}
	for _, c := range cases {
		s := newTestServer(t, nil, nil)
		h := s.Handler()
		doc := getSettings(t, h)
		c.mutate(&doc, t)
		rec := putSettings(t, h, doc)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: код %d, ожидался 400: %s", c.name, rec.Code, rec.Body.String())
			continue
		}
		msg := rec.Body.String()
		if !strings.Contains(msg, c.field) || !cyrillicRe.MatchString(msg) {
			t.Errorf("%s: сообщение %q должно быть по-русски и называть поле %q", c.name, msg, c.field)
		}
		if _, err := os.Stat(s.Paths.Config); err == nil {
			t.Errorf("%s: после отказа появился файл настроек", c.name)
		}
	}
}

func f64(v float64) *float64 { return &v }

// Допустимые крайности принимаются: границы включены, а температура
// допустима там, где модель обычная.
func TestPutSettingsAcceptsBoundaries(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	doc := getSettings(t, h)
	doc.CorpusK = 20
	ds := profileOf(t, &doc, "deepseek")
	ds.Temperature, ds.MaxOutputTokens, ds.ContextBudget = f64(2), 1_000_000, 0
	ds.Prices = &config.Prices{Input: 0, Output: 1e6}
	cl := profileOf(t, &doc, "claude")
	cl.Temperature = f64(1)
	oa := profileOf(t, &doc, "openai") // рассуждающая: усилие можно
	oa.ReasoningEffort = "high"
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	cfg, _ := config.Load(s.Paths.Config)
	if *cfg.Profiles["deepseek"].Temperature != 2 || *cfg.Profiles["claude"].Temperature != 1 ||
		cfg.Profiles["openai"].ReasoningEffort != "high" || cfg.CorpusK != 20 {
		t.Fatalf("конфиг %+v", cfg)
	}
}

// Неизвестные поля отвергаются, а не теряются молча: опечатка в названии
// поля иначе сохранила бы настройку, которой нет.
func TestPutSettingsRejectsUnknownFieldsAndBadJSON(t *testing.T) {
	ok := `{"name":"deepseek","kind":"openai","base_url":"https://x","model":"m"`
	cases := map[string]string{
		"лишнее поле верхнего уровня": `{"default":"deepseek","corpus_k":6,"extra":1,"profiles":[` + ok + `}]}`,
		"опечатка в поле профиля":     `{"default":"deepseek","corpus_k":6,"profiles":[` + ok + `,"temprature":1}]}`,
		"лишнее поле в ценах":         `{"default":"deepseek","corpus_k":6,"profiles":[` + ok + `,"prices":{"input":1,"cost":2}}]}`,
		"лишнее поле в ключе":         `{"default":"deepseek","corpus_k":6,"profiles":[` + ok + `,"key":{"source":"none","secret":"x"}}]}`,
		"не тот тип":                  `{"default":"deepseek","corpus_k":"шесть","profiles":[` + ok + `}]}`,
		"не JSON":                     `это не JSON`,
		"хвост после JSON":            `{"default":"deepseek","corpus_k":6,"profiles":[` + ok + `}]}{}`,
	}
	for name, body := range cases {
		s := newTestServer(t, nil, nil)
		rec := do(t, s.Handler(), http.MethodPut, "/api/settings", body)
		if rec.Code != http.StatusBadRequest || !cyrillicRe.MatchString(rec.Body.String()) {
			t.Errorf("%s: код %d, ответ %q", name, rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(s.Paths.Config); err == nil {
			t.Errorf("%s: появился файл настроек", name)
		}
	}
}

// Круг интерфейса: то, что GET отдал, PUT принимает как есть (вместе с
// builtin и key), а ненарушенные настройки остаются пустым конфигом.
func TestPutSettingsRoundTripKeepsConfigEmpty(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	if rec := putSettings(t, h, getSettings(t, h)); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(s.Paths.Config)
	if string(data) != "{}\n" {
		t.Fatalf("config.json = %q, ожидалось {}", data)
	}
}

// Поле key в PUT не читается: присланное — не ключ, и keys.json не создаётся.
func TestPutSettingsIgnoresKeyField(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	doc := getSettings(t, h)
	doc.Profiles[0].Key = keyView{Source: "file", Hint: "…zzzz", Warning: "что угодно"}
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(s.Paths.Keys); err == nil {
		t.Fatal("PUT настроек создал keys.json")
	}
	if got := getSettings(t, h).Profiles[0].Key; got.Source != "none" {
		t.Fatalf("ключ профиля после PUT: %+v", got)
	}
}

// Встроенный профиль, которого нет в присланном списке, возвращается со
// своими умолчаниями (config.Save пишет лишь отличия), а профилем по умолчанию
// может быть и такой.
func TestPutSettingsOmittedBuiltinComesBackWithDefaults(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	writeConfig(t, s, `{"profiles":{"claude":{"model":"особая"}}}`)
	doc := getSettings(t, h)
	var only []profileView
	for _, p := range doc.Profiles {
		if p.Name == "deepseek" {
			only = append(only, p)
		}
	}
	doc.Profiles, doc.Default = only, "claude"
	rec := putSettings(t, h, doc)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	after := getSettings(t, h)
	if len(after.Profiles) != len(config.Builtin().Profiles) || after.Default != "claude" {
		t.Fatalf("профили %d, по умолчанию %q", len(after.Profiles), after.Default)
	}
	if m := profileOf(t, &after, "claude").Model; m != config.Builtin().Profiles["claude"].Model {
		t.Fatalf("claude не сброшен к умолчанию: %q", m)
	}
}

// savedLocalProfiles заводит профили local и other с ключами (и ключ claude).
func savedLocalProfiles(t *testing.T, s *Server) http.Handler {
	t.Helper()
	h := s.Handler()
	doc := getSettings(t, h)
	doc.Profiles = append(doc.Profiles,
		profileView{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m"},
		profileView{Name: "other", Kind: "openai", BaseURL: "http://127.0.0.1:2/v1", Model: "m"})
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	st := keys.Store{Path: s.Paths.Keys}
	for _, n := range []string{"local", "other", "claude"} {
		if err := st.Set(n, "ключ-"+n); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func withoutProfile(doc settingsDoc, name string) settingsDoc {
	var kept []profileView
	for _, p := range doc.Profiles {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	doc.Profiles = kept
	return doc
}

func keyOf(t *testing.T, s *Server, name string) string {
	t.Helper()
	k, err := (keys.Store{Path: s.Paths.Keys}).Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// Убранный профиль уносит с собой ключ из keys.json, но только когда
// пользователь убрал его сам и сказал об этом в removed; остальные ключи целы.
func TestPutSettingsDropsKeyOfExplicitlyRemovedProfile(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := savedLocalProfiles(t, s)
	doc := withoutProfile(getSettings(t, h), "local")
	doc.Removed = []string{"local"}
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	for n, want := range map[string]string{"local": "", "other": "ключ-other", "claude": "ключ-claude"} {
		if got := keyOf(t, s, n); got != want {
			t.Errorf("ключ %s: %q, ожидалось %q", n, got, want)
		}
	}
}

// PUT заменяет документ целиком и версии не проверяет: вторая вкладка с
// черновиком, который старше добавленного профиля, уберёт профиль из
// настроек, но ключ не тронет — по отсутствию в документе ничего не удаляется.
func TestPutSettingsStaleDraftDoesNotDeleteKeys(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := savedLocalProfiles(t, s)
	stale := withoutProfile(getSettings(t, h), "local") // removed не назван
	if rec := putSettings(t, h, stale); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if got := keyOf(t, s, "local"); got != "ключ-local" {
		t.Fatalf("ключ профиля, не названного в removed: %q", got)
	}
	// профиль завели заново — ключ на месте
	doc := getSettings(t, h)
	doc.Profiles = append(doc.Profiles, profileView{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m"})
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if k := profileOf(t, ptr(getSettings(t, h)), "local").Key; k.Source != "file" {
		t.Fatalf("ключ после повторного добавления: %+v", k)
	}
}

func ptr[T any](v T) *T { return &v }

// Имя в removed, которое всё ещё есть в документе, ключ не удаляет.
func TestPutSettingsRemovedButStillListedKeepsKey(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := savedLocalProfiles(t, s)
	doc := getSettings(t, h)
	doc.Removed = []string{"local"}
	if rec := putSettings(t, h, doc); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if got := keyOf(t, s, "local"); got != "ключ-local" {
		t.Fatalf("ключ профиля, оставшегося в настройках: %q", got)
	}
}

// removed проверяется до записи: отказ — 400 с названием поля, на диске ни
// настроек, ни ключей не меняется.
func TestPutSettingsValidatesRemoved(t *testing.T) {
	cases := map[string][]string{
		"неизвестное имя":    {"нет-такого"},
		"встроенный профиль": {"claude"},
		"имя дважды":         {"local", "local"},
		"пустое имя":         {""},
		"слишком много имён": make([]string, maxProfiles+1),
	}
	for i := range cases["слишком много имён"] {
		cases["слишком много имён"][i] = fmt.Sprintf("p%d", i)
	}
	for name, removed := range cases {
		s := newTestServer(t, nil, nil)
		h := savedLocalProfiles(t, s)
		cfgBefore, _ := os.ReadFile(s.Paths.Config)
		keysBefore, _ := os.ReadFile(s.Paths.Keys)
		doc := withoutProfile(getSettings(t, h), "local")
		doc.Removed = removed
		rec := putSettings(t, h, doc)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "removed") || !cyrillicRe.MatchString(rec.Body.String()) {
			t.Errorf("%s: код %d: %.160s", name, rec.Code, rec.Body.String())
		}
		if cfgAfter, _ := os.ReadFile(s.Paths.Config); !bytes.Equal(cfgBefore, cfgAfter) {
			t.Errorf("%s: настройки изменились", name)
		}
		if keysAfter, _ := os.ReadFile(s.Paths.Keys); !bytes.Equal(keysBefore, keysAfter) {
			t.Errorf("%s: ключи изменились", name)
		}
	}
	// не список
	s := newTestServer(t, nil, nil)
	rec := do(t, s.Handler(), http.MethodPut, "/api/settings",
		`{"default":"deepseek","corpus_k":6,"removed":"local","profiles":[{"name":"deepseek","kind":"openai","base_url":"https://x","model":"m"}]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "removed") {
		t.Errorf("removed не списком: код %d: %s", rec.Code, rec.Body.String())
	}
}

// Ответ настроек не несёт ни ключей, ни их частей, кроме подсказки из четырёх знаков.
func TestSettingsNeverReturnKeyMaterial(t *testing.T) {
	const envSecret, fileSecret = "zq-секрет-окружения-5521", "zq-секрет-файла-8830"
	s := newTestServer(t, nil, nil)
	t.Setenv("DEEPSEEK_API_KEY", envSecret)
	if err := (keys.Store{Path: s.Paths.Keys}).Set("claude", fileSecret); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for _, rec := range []*httptest.ResponseRecorder{
		do(t, h, http.MethodGet, "/api/settings", ""),
		putSettings(t, h, getSettings(t, h)),
		do(t, h, http.MethodGet, "/api/state", ""),
	} {
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Fatalf("код %d: %s", rec.Code, body)
		}
		for _, leak := range []string{envSecret, fileSecret, "секрет", "zq-"} {
			if strings.Contains(body, leak) {
				t.Errorf("в ответе есть %q", leak)
			}
		}
	}
}

// deepseek-flash делит с deepseek переменную ключа: с ней оба «env» и с той же
// подсказкой. Ключ из keys.json — по имени профиля: у deepseek он «file», а у
// deepseek-flash его нет, пока не сохранён свой.
func TestDeepseekFlashKeySourceInStateAndSettings(t *testing.T) {
	const secret = "zq-ключ-deepseek-4417"
	for _, path := range []string{"/api/state", "/api/settings"} {
		s := newTestServer(t, nil, nil)
		h := s.Handler()
		get := func() map[string]keyView {
			rec := do(t, h, http.MethodGet, path, "")
			var doc struct {
				Profiles []struct {
					Name    string  `json:"name"`
					Builtin bool    `json:"builtin"`
					Key     keyView `json:"key"`
				} `json:"profiles"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || rec.Code != 200 {
				t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
			}
			out := map[string]keyView{}
			for _, p := range doc.Profiles {
				out[p.Name] = p.Key
				if p.Name == "deepseek-flash" && !p.Builtin {
					t.Errorf("%s: deepseek-flash не помечен встроенным", path)
				}
			}
			return out
		}
		if k := get(); k["deepseek-flash"].Source != "none" || k["deepseek"].Source != "none" {
			t.Fatalf("%s: без ключей %+v", path, k)
		}
		if err := (keys.Store{Path: s.Paths.Keys}).Set("deepseek", secret); err != nil {
			t.Fatal(err)
		}
		if k := get(); k["deepseek"].Source != "file" || k["deepseek-flash"].Source != "none" {
			t.Fatalf("%s: ключ файла лежит под именем deepseek: %+v", path, k)
		}
		t.Setenv("DEEPSEEK_API_KEY", secret)
		k := get()
		if k["deepseek"] != (keyView{Source: "env", Hint: "…4417"}) || k["deepseek-flash"] != k["deepseek"] {
			t.Fatalf("%s: с переменной окружения %+v", path, k)
		}
		t.Setenv("DEEPSEEK_API_KEY", "")
	}
}

// ---- ключи ----

func TestKeysEndpoints(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	if rec := do(t, h, http.MethodPut, "/api/keys/claude", `{"key":"sk-ant-новый-9z8y"}`); rec.Code != 200 {
		t.Fatalf("код %d", rec.Code)
	}
	if k, _ := (keys.Store{Path: s.Paths.Keys}).Get("claude"); k != "sk-ant-новый-9z8y" {
		t.Fatalf("ключ %q", k)
	}
	if rec := do(t, h, http.MethodPut, "/api/keys/нет-такого", `{"key":"x1234"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("неизвестный профиль: код %d", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/api/keys/claude", ""); rec.Code != 200 {
		t.Fatalf("удаление: код %d", rec.Code)
	}
	if k, _ := (keys.Store{Path: s.Paths.Keys}).Get("claude"); k != "" {
		t.Fatal("ключ не удалён")
	}
	if strings.Contains(do(t, h, http.MethodGet, "/api/settings", "").Body.String(), "sk-ant") {
		t.Fatal("ключ в ответе настроек")
	}
}

func TestKeyEndpointsUnknownProfile(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	for _, c := range [][2]string{{http.MethodPut, `{"key":"x1234"}`}, {http.MethodDelete, ""}, {http.MethodPost, ""}} {
		path := "/api/keys/нет-такого"
		if c[0] == http.MethodPost {
			path += "/check"
		}
		if rec := do(t, h, c[0], path, c[1]); rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404", c[0], rec.Code)
		}
	}
}

// Ключ с пробелом или переводом строки внутри — след неудачной вставки;
// по HTTP-заголовку он вообще не уйдёт. Отказ не повторяет ключа.
func TestPutKeyRejectsMalformedKeys(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	cases := map[string]string{
		"пустой":           `{"key":""}`,
		"одни пробелы":     `{"key":"   "}`,
		"пробел внутри":    `{"key":"sk-aaa bbb"}`,
		"перевод строки":   `{"key":"sk-aaa\nsk-bbb"}`,
		"управляющий знак": `{"key":"sk-aaa\u0000bbb"}`,
		"слишком длинный":  `{"key":"` + strings.Repeat("k", 5000) + `"}`,
		"не строка":        `{"key":12345678}`,
		"лишнее поле":      `{"key":"sk-aaa-bbb-ccc","force":true}`,
		"не JSON":          `sk-aaa-bbb-ccc`,
	}
	for name, body := range cases {
		rec := do(t, h, http.MethodPut, "/api/keys/claude", body)
		if rec.Code != http.StatusBadRequest || !cyrillicRe.MatchString(rec.Body.String()) {
			t.Errorf("%s: код %d, ответ %q", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "bbb") || strings.Contains(rec.Body.String(), "kkkk") {
			t.Errorf("%s: ключ повторён в ответе", name)
		}
	}
	if _, err := os.Stat(s.Paths.Keys); err == nil {
		t.Fatal("отказ создал keys.json")
	}
}

// Ключ профиля, заданного переменной окружения, интерфейсом не меняется:
// сохранённый поверх него ключ молча не действовал бы.
func TestPutKeyRefusedForEnvProfile(t *testing.T) {
	s := newTestServer(t, nil, nil)
	t.Setenv("DEEPSEEK_API_KEY", "zq-из-окружения-1234")
	h := s.Handler()
	rec := do(t, h, http.MethodPut, "/api/keys/deepseek", `{"key":"sk-другой-ключ"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "DEEPSEEK_API_KEY") {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(s.Paths.Keys); err == nil {
		t.Fatal("keys.json создан")
	}
	// удалить старый файловый ключ можно: окружение его всё равно перекрывает
	if err := (keys.Store{Path: s.Paths.Keys}).Set("deepseek", "старый-файловый"); err != nil {
		t.Fatal(err)
	}
	if rec := do(t, h, http.MethodDelete, "/api/keys/deepseek", ""); rec.Code != 200 {
		t.Fatalf("удаление: код %d", rec.Code)
	}
	if k, _ := (keys.Store{Path: s.Paths.Keys}).Get("deepseek"); k != "" {
		t.Fatal("файловый ключ не удалён")
	}
}

// Замена ключей идёт чтением и записью одного файла; без общего замка
// параллельные запросы теряют чужие правки.
func TestKeysConcurrentWritesLoseNothing(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	const n = 24
	var profiles strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&profiles, `%s"p%d":{"kind":"openai","base_url":"http://127.0.0.1:1/v1","model":"m"}`, map[bool]string{true: "", false: ","}[i == 0], i)
	}
	writeConfig(t, s, `{"profiles":{`+profiles.String()+`}}`)
	st := keys.Store{Path: s.Paths.Keys}
	if err := st.Set("claude", "уцелеть-должен"); err != nil {
		t.Fatal(err)
	}
	if err := st.Set("p0", "будет-удалён"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rec := do(t, h, http.MethodPut, fmt.Sprintf("/api/keys/p%d", i), fmt.Sprintf(`{"key":"ключ-%d"}`, i)); rec.Code != 200 {
				t.Errorf("p%d: код %d: %s", i, rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if rec := do(t, h, http.MethodDelete, "/api/keys/p0", ""); rec.Code != 200 {
			t.Errorf("удаление p0: код %d", rec.Code)
		}
	}()
	wg.Wait()
	for i := 1; i < n; i++ {
		if got, _ := st.Get(fmt.Sprintf("p%d", i)); got != fmt.Sprintf("ключ-%d", i) {
			t.Errorf("p%d: ключ %q — правка потеряна", i, got)
		}
	}
	if got, _ := st.Get("p0"); got != "" {
		t.Errorf("p0 не удалён: %q", got)
	}
	if got, _ := st.Get("claude"); got != "уцелеть-должен" {
		t.Errorf("claude: %q", got)
	}
}

// ---- испорченный keys.json ----

const corruptKeys = `{"profiles": {"claude": "sk-ant-в-битом-файле-7777", ТУТ ПОЛОМКА`

func corruptKeysServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	if err := os.MkdirAll(filepath.Dir(s.Paths.Keys), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Paths.Keys, []byte(corruptKeys), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

// Экран, который чинит ключи, должен открываться и при испорченном файле:
// профили без ключа в окружении получают source none и предупреждение, ни
// ключа, ни пути в нём нет.
func TestCorruptKeysFileDegradesPerProfile(t *testing.T) {
	s := corruptKeysServer(t)
	t.Setenv("OPENAI_API_KEY", "zq-из-окружения-6060")
	h := s.Handler()
	for _, path := range []string{"/api/state", "/api/settings"} {
		rec := do(t, h, http.MethodGet, path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: код %d: %s", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, leak := range []string{s.Paths.Keys, filepath.Dir(s.Paths.Keys), "sk-ant", "7777", "invalid character", "ПОЛОМКА"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: в ответе есть %q", path, leak)
			}
		}
		var doc struct {
			Profiles []struct {
				Name string  `json:"name"`
				Key  keyView `json:"key"`
			} `json:"profiles"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || len(doc.Profiles) != len(config.Builtin().Profiles) {
			t.Fatalf("%s: %v, профилей %d", path, err, len(doc.Profiles))
		}
		for _, p := range doc.Profiles {
			if p.Name == "openai" { // ключ из окружения: файл не читается, предупреждения нет
				if p.Key != (keyView{Source: "env", Hint: "…6060"}) {
					t.Errorf("%s: openai %+v", path, p.Key)
				}
				continue
			}
			if p.Key != (keyView{Source: "none", Warning: "keys.json не читается"}) {
				t.Errorf("%s: %s %+v", path, p.Name, p.Key)
			}
		}
	}
	// починили файл — предупреждение ушло
	os.Remove(s.Paths.Keys)
	if strings.Contains(do(t, h, http.MethodGet, "/api/settings", "").Body.String(), "не читается") {
		t.Error("предупреждение осталось после починки")
	}
}

// Запись ключа в испорченный файл — отказ с понятным текстом; сам файл не
// тронут ни байтом и временных файлов рядом не остаётся.
func TestCorruptKeysFileIsNeverOverwritten(t *testing.T) {
	s := corruptKeysServer(t)
	h := s.Handler()
	before, _ := os.ReadFile(s.Paths.Keys)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPut, "/api/keys/claude", `{"key":"sk-ant-новый-ключ-1111"}`},
		{http.MethodDelete, "/api/keys/claude", ""},
	} {
		rec := do(t, h, c.method, c.path, c.body)
		msg := rec.Body.String()
		if rec.Code != http.StatusConflict || !strings.Contains(msg, "keys.json не читается") || !cyrillicRe.MatchString(msg) {
			t.Errorf("%s: код %d, ответ %q", c.method, rec.Code, msg)
		}
		for _, leak := range []string{s.Paths.Keys, "invalid character", "1111", "7777"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s: в ответе %q", c.method, leak)
			}
		}
		after, _ := os.ReadFile(s.Paths.Keys)
		if !bytes.Equal(before, after) {
			t.Fatalf("%s: испорченный файл перезаписан: %q", c.method, after)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(s.Paths.Keys))
	if len(entries) != 1 {
		t.Errorf("рядом остались лишние файлы: %v", entries)
	}
}

// Профиль с неверным типом в config.json не обрушивает состояние: экран
// настроек должен открыться, чтобы тип можно было исправить.
func TestBadKindInConfigDoesNotBreakState(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, `{"profiles":{"странный":{"kind":"gemini","model":"m"}}}`)
	for _, path := range []string{"/api/state", "/api/settings"} {
		rec := do(t, s.Handler(), http.MethodGet, path, "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"странный"`) {
			t.Fatalf("%s: код %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}

// ---- проверка ключа: только поддельные провайдеры httptest ----

type checkResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func postCheck(t *testing.T, h http.Handler, name string) checkResult {
	t.Helper()
	rec := do(t, h, http.MethodPost, "/api/keys/"+name+"/check", "")
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var r checkResult
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func localProfile(t *testing.T, s *Server, url, keyEnv string) {
	t.Helper()
	writeConfig(t, s, fmt.Sprintf(`{"profiles":{"local":{"kind":"openai","base_url":%q,"model":"m","key_env":%q}}}`, url, keyEnv))
}

func TestCheckKeyEndpoint(t *testing.T) {
	var hits atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer верный-ключ-0001":
			w.Write([]byte(`{"data":[]}`))
		case "Bearer сломан-у-провайдера":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer fake.Close()
	s := newTestServer(t, nil, nil)
	localProfile(t, s, fake.URL+"/v1", "")
	h := s.Handler()
	st := keys.Store{Path: s.Paths.Keys}

	// ключа нет — запроса к провайдеру нет
	if r := postCheck(t, h, "local"); r.OK || !strings.Contains(r.Message, "local") || hits.Load() != 0 {
		t.Fatalf("без ключа: %+v, обращений %d", r, hits.Load())
	}
	st.Set("local", "верный-ключ-0001")
	if r := postCheck(t, h, "local"); !r.OK || r.Status != 200 || r.Message != "ключ принят" {
		t.Fatalf("верный: %+v", r)
	}
	st.Set("local", "чужой-ключ-0002")
	if r := postCheck(t, h, "local"); r.OK || r.Status != 401 || !strings.Contains(r.Message, "отклонён") || !strings.Contains(r.Message, "401") {
		t.Fatalf("чужой: %+v", r)
	}
	st.Set("local", "сломан-у-провайдера")
	if r := postCheck(t, h, "local"); r.OK || r.Status != 500 || strings.Contains(r.Message, "отклонён") || !cyrillicRe.MatchString(r.Message) {
		t.Fatalf("ошибка провайдера не равна отказу по ключу: %+v", r)
	}
}

// Профиль с ключом из окружения: проверяется тот ключ, что реально
// сработает.
func TestCheckKeyUsesEnvironmentKey(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer из-окружения-3003" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer fake.Close()
	s := newTestServer(t, nil, nil)
	t.Setenv("ARISTOTLE_TEST_LOCAL_KEY", "из-окружения-3003")
	localProfile(t, s, fake.URL, "ARISTOTLE_TEST_LOCAL_KEY")
	(keys.Store{Path: s.Paths.Keys}).Set("local", "файловый-не-тот")
	if r := postCheck(t, s.Handler(), "local"); !r.OK {
		t.Fatalf("%+v", r)
	}
}

// Недоступный провайдер, перенаправление и молчание — отказ с русским
// текстом без адреса и ключа.
func TestCheckKeyFailures(t *testing.T) {
	const secret = "ключ-для-отказов-9090"
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
		localProfile(t, s, url, "")
		(keys.Store{Path: s.Paths.Keys}).Set("local", secret)
		r := postCheck(t, s.Handler(), "local")
		if r.OK || !cyrillicRe.MatchString(r.Message) {
			t.Errorf("%s: %+v", name, r)
		}
		for _, leak := range []string{secret, url, "127.0.0.1"} {
			if strings.Contains(r.Message, leak) {
				t.Errorf("%s: в сообщении %q", name, leak)
			}
		}
	}
	if leaked.Load() != nil {
		t.Fatal("ключ ушёл по перенаправлению на чужой адрес")
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("ключ в журнале сервера:\n%s", logs.String())
	}
}

// Испорченный keys.json: проверка отвечает отказом, не 500 и не путём к файлу.
func TestCheckKeyWithCorruptKeysFile(t *testing.T) {
	s := corruptKeysServer(t)
	r := postCheck(t, s.Handler(), "claude")
	if r.OK || !strings.Contains(r.Message, "keys.json не читается") || strings.Contains(r.Message, s.Paths.Keys) {
		t.Fatalf("%+v", r)
	}
}

// Ключ не попадает ни в один ответ и ни в одну строку журнала сервера, что бы
// с ним ни делали: сохранили, проверили, отвергли, прочитали настройки.
func TestKeyNeverInResponsesOrLogs(t *testing.T) {
	const secret = "zq-нигде-не-должен-появиться-5151"
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// провайдер, который честно отвечает 401 и эхом возвращает заголовок
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("плохой ключ: " + r.Header.Get("Authorization")))
	}))
	defer fake.Close()
	s := newTestServer(t, nil, nil)
	localProfile(t, s, fake.URL, "")
	h := s.Handler()
	var all strings.Builder
	for _, rec := range []*httptest.ResponseRecorder{
		do(t, h, http.MethodPut, "/api/keys/local", `{"key":"`+secret+`"}`),
		do(t, h, http.MethodPost, "/api/keys/local/check", ""),
		do(t, h, http.MethodGet, "/api/settings", ""),
		do(t, h, http.MethodGet, "/api/state", ""),
		do(t, h, http.MethodPut, "/api/keys/local", `{"key":"`+secret+` с пробелом"}`),
		do(t, h, http.MethodDelete, "/api/keys/local", ""),
	} {
		all.WriteString(rec.Body.String())
		all.WriteString("\n")
	}
	all.WriteString(logs.String())
	if strings.Contains(all.String(), "нигде-не-должен") || strings.Contains(all.String(), secret) {
		t.Fatalf("ключ просочился:\n%s", all.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Профиль Anthropic с правленным вручную base_url: проверка идёт на фиксированный
// адрес API (здесь его перехватывает транспорт клиента — сети нет), а не туда.
func TestCheckKeyEndpointAnthropicIgnoresBaseURL(t *testing.T) {
	var trapHit atomic.Bool
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { trapHit.Store(true) }))
	defer trap.Close()
	s := newTestServer(t, nil, nil)
	writeConfig(t, s, fmt.Sprintf(`{"profiles":{"claude":{"base_url":%q}}}`, trap.URL))
	(keys.Store{Path: s.Paths.Keys}).Set("claude", "sk-ant-fake-for-test")
	var urls []string
	s.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})}
	if r := postCheck(t, s.Handler(), "claude"); !r.OK {
		t.Fatalf("%+v", r)
	}
	if len(urls) != 1 || urls[0] != "https://api.anthropic.com/v1/models" || trapHit.Load() {
		t.Fatalf("запросы %v, base_url получил запрос: %v", urls, trapHit.Load())
	}
}
