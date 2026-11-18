package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// scriptedProvider отдаёт заранее записанные потоки, по одному на запрос.
type scriptedProvider struct {
	mu    sync.Mutex
	turns [][]provider.Event
}

func (p *scriptedProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.turns) == 0 {
		return nil, errors.New("сценарий кончился")
	}
	events := p.turns[0]
	p.turns = p.turns[1:]
	return &scriptedStream{events: events}, nil
}

type scriptedStream struct {
	events []provider.Event
	cur    provider.Event
}

func (s *scriptedStream) Next() bool {
	if len(s.events) == 0 {
		return false
	}
	s.cur, s.events = s.events[0], s.events[1:]
	return true
}
func (s *scriptedStream) Event() provider.Event { return s.cur }
func (s *scriptedStream) Err() error            { return nil }
func (s *scriptedStream) Close() error          { return nil }

type fakeFinder struct{ hits []corpus.Hit }

func (f fakeFinder) Search(context.Context, string, string, int) ([]corpus.Hit, error) {
	return f.hits, nil
}

// isolateKeyEnv обнуляет переменные окружения с ключами всех встроенных
// профилей: тест не должен ни зависеть от настоящих ключей разработчика, ни
// выводить их (подсказка ключа попадает в /api/state). Список берётся из
// config.Builtin, чтобы новый профиль не остался в стороне.
func isolateKeyEnv(t *testing.T) {
	t.Helper()
	for _, p := range config.Builtin().Profiles {
		t.Setenv(p.KeyEnv, "")
	}
}

// newTestServer — сервер на временных каталогах, с подменённым провайдером
// и поиском; порт 8337 — для проверки Host и Origin. Окружение с ключами
// обнулено; тесту, которому нужен ключ в окружении, достаточно t.Setenv
// после вызова.
func newTestServer(t *testing.T, prov provider.Provider, finder chat.Finder) *Server {
	t.Helper()
	isolateKeyEnv(t)
	home := t.TempDir()
	s := New(t.TempDir(), app.Paths{
		Config:  filepath.Join(home, "config.json"),
		Keys:    filepath.Join(home, "keys.json"),
		History: filepath.Join(home, "history"),
	}, 8337)
	s.System = "ты Аристотель"
	s.NewProvider = func(config.Profile) (provider.Provider, error) { return prov, nil }
	s.Finder = finder
	return s
}

// do — запрос так, как его шлёт браузер со страницы интерфейса.
func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = "127.0.0.1:8337"
	if method != http.MethodGet {
		req.Header.Set("Origin", "http://127.0.0.1:8337")
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
