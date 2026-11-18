package web

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/keys"
)

func TestGuardRejectsForeignHost(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Host = "evil.example:8337"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("код %d, ожидался 421", rec.Code)
	}
}

func TestGuardRejectsPostFromElsewhere(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	for _, origin := range []string{"", "http://evil.example", "http://127.0.0.1:9999"} {
		req := httptest.NewRequest(http.MethodPost, "/api/state", nil)
		req.Host = "127.0.0.1:8337"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Origin %q: код %d, ожидался 403", origin, rec.Code)
		}
	}
}

func TestGuardAcceptsSameOriginWithoutOriginHeader(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/state", nil)
	req.Host = "localhost:8337"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusMisdirectedRequest {
		t.Fatalf("код %d: запрос со своей страницы отвергнут", rec.Code)
	}
}

func TestIndexIsServed(t *testing.T) {
	rec := do(t, newTestServer(t, nil, nil).Handler(), http.MethodGet, "/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Аристотель</title>") {
		t.Fatalf("код %d, тело %.200s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("нет Content-Security-Policy")
	}
}

func TestFontsAreServedAsWoff2(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	for _, name := range []string{"forum", "eb-garamond", "eb-garamond-italic"} {
		rec := do(t, h, http.MethodGet, "/static/fonts/"+name+".woff2", "")
		if rec.Code != 200 {
			t.Fatalf("%s: код %d, ожидался 200", name, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "font/woff2" {
			t.Errorf("%s: Content-Type %q, ожидался font/woff2", name, got)
		}
	}
	// В таблице типов Go woff2 нет, а системный mime.types есть не везде.
	// Файловый сервер выручил бы и сниффинг сигнатуры wOF2, но полагаться
	// на него не стоит: тип задан явно, и тест его проверяет напрямую.
	if got := mime.TypeByExtension(".woff2"); got != "font/woff2" {
		t.Errorf("тип расширения .woff2 — %q, ожидался font/woff2", got)
	}
}

func TestStaticDirectoriesAreNotListed(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	// Без index.html каталог — не страница: список файлов наружу не отдаём,
	// ни со слешем, ни без него (без слеша FileServer сначала редиректит).
	for _, path := range []string{"/static/js/", "/static/fonts/", "/static/img/", "/static/js", "/static/fonts"} {
		rec := do(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: код %d, ожидался 404; тело %.120s", path, rec.Code, rec.Body.String())
		}
	}
	// Сами файлы по-прежнему отдаются.
	if rec := do(t, h, http.MethodGet, "/static/fonts/OFL-Forum.txt", ""); rec.Code != 200 {
		t.Errorf("файл внутри каталога: код %d, ожидался 200", rec.Code)
	}
}

func TestStateNeverShowsKeys(t *testing.T) {
	// Выдуманные значения без вендорского префикса: ни за настоящий ключ, ни
	// за образец формата их не принять, и утечку без префикса они тоже ловят.
	const (
		envSecret  = "zq-выдуманный-секрет-из-окружения-7731"
		fileSecret = "zq-выдуманный-секрет-из-файла-4096"
	)
	s := newTestServer(t, nil, nil) // обнуляет окружение с ключами
	t.Setenv("DEEPSEEK_API_KEY", envSecret)
	if err := (keys.Store{Path: s.Paths.Keys}).Set("claude", fileSecret); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s.Handler(), http.MethodGet, "/api/state", "")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}

	// Ключ целиком и всё, кроме последних четырёх символов, наружу не идёт;
	// пятый с конца символ вместе с подсказкой уже указал бы на лишнее.
	for name, secret := range map[string]string{"из окружения": envSecret, "из файла": fileSecret} {
		r := []rune(secret)
		for _, leak := range []string{secret, string(r[:len(r)-4]), string(r[len(r)-5:])} {
			if strings.Contains(body, leak) {
				t.Errorf("ключ %s: в ответе есть %d символов ключа, наружу идут только последние четыре", name, len([]rune(leak)))
			}
		}
	}
	if strings.Contains(body, "секрет") {
		t.Error("в ответе есть слово из ключа")
	}

	var state struct {
		Profiles []struct {
			Name string  `json:"name"`
			Key  keyView `json:"key"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatalf("ответ не разобрать: %v", err)
	}
	got := map[string]keyView{}
	for _, p := range state.Profiles {
		got[p.Name] = p.Key
	}
	want := map[string]keyView{
		"deepseek":   {Source: "env", Hint: "…7731"},
		"claude":     {Source: "file", Hint: "…4096"},
		"openai":     {Source: "none"},
		"openrouter": {Source: "none"},
	}
	for name, w := range want {
		// Подсказку в сообщение не кладём: при утечке там оказался бы ключ.
		if got[name].Source != w.Source {
			t.Errorf("профиль %s: источник ключа %q, ожидался %q", name, got[name].Source, w.Source)
		}
		if got[name].Hint != w.Hint {
			t.Errorf("профиль %s: подсказка ключа не совпала с ожидаемой", name)
		}
	}
}
