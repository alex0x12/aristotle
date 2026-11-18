package web

import (
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

func TestStateNeverShowsKeys(t *testing.T) {
	s := newTestServer(t, nil, nil)
	if err := (keys.Store{Path: s.Paths.Keys}).Set("claude", "sk-ant-секрет-a1b2"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	rec := do(t, s.Handler(), http.MethodGet, "/api/state", "")
	body := rec.Body.String()
	if rec.Code != 200 || strings.Contains(body, "sk-ant") {
		t.Fatalf("код %d; ключ утёк в ответ: %s", rec.Code, body)
	}
	if !strings.Contains(body, `"source":"file"`) || !strings.Contains(body, `"hint":"…a1b2"`) {
		t.Fatalf("нет сведений о ключе: %s", body)
	}
}
