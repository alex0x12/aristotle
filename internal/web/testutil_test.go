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

// testKey — ключ, который newTestServer кладёт в keys.json: заведомо
// выдуманный и узнаваемый, чтобы тесты на утечку искали именно его.
const testKey = "FAKE-TESTKEY-w9Q2-x7Lm-4f1d"

// newTestServer — сервер на временных каталогах, с подменённым провайдером и
// поиском; порт 8337 — для проверки Host и Origin. В keys.json лежит testKey:
// ход без ключа сервер отвергает сам, до провайдера, поэтому тестам с ходом он
// нужен; тесту без ключа достаточно noKey. Переменные окружения сервер не читает
// вовсе (это проверяет TestEnvironmentKeysAreIgnored), так что изолировать их не
// нужно.
func newTestServer(t *testing.T, prov provider.Provider, finder chat.Finder) *Server {
	t.Helper()
	home := t.TempDir()
	s := New(t.TempDir(), app.Paths{
		Config:  filepath.Join(home, "config.json"),
		Keys:    filepath.Join(home, "keys.json"),
		History: filepath.Join(home, "history"),
	}, 8337)
	s.System = "ты Аристотель"
	s.NewProvider = func(app.Connection) (provider.Provider, error) { return prov, nil }
	s.Finder = finder
	if err := s.keyStore().Set(testKey); err != nil {
		t.Fatal(err)
	}
	return s
}

// noKey убирает ключ из keys.json.
func noKey(t *testing.T, s *Server) {
	t.Helper()
	if err := s.keyStore().Delete(); err != nil {
		t.Fatal(err)
	}
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
