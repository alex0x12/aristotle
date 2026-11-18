// Package web — локальный веб-интерфейс Аристотеля: беседы, источники,
// библиотека корпуса, настройки. Слушает только петлю (cmd/aristotle/web.go).
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/provider"
)

//go:embed static
var embedded embed.FS

type Server struct {
	Root  string // корень репозитория: persona/ и corpus/
	Paths app.Paths
	Port  int // для проверки Host и Origin

	// Подмены для тестов; nil и пусто — настоящие.
	NewProvider func(config.Profile) (provider.Provider, error)
	Finder      chat.Finder
	System      string
	HTTPClient  *http.Client

	mu    sync.Mutex
	convs map[string]*conv

	worksMu sync.Mutex
	works   []workRef
}

// Заглушка до задачи 9: поле Server ссылается на этот тип, а настоящее
// определение (library.go) появится позже. При добавлении настоящего типа
// заглушку рядом удалить.
type workRef struct{}

func New(root string, paths app.Paths, port int) *Server {
	return &Server{Root: root, Paths: paths, Port: port, convs: map[string]*conv{}}
}

func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err) // встроенное дерево есть всегда; ошибка — дефект сборки
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "index.html")
	})
	s.routes(mux)
	return guard(s.Port, mux)
}

// routes — маршруты API. Каждая задача плана добавляет сюда свои.
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/conversations", s.handleList)
	mux.HandleFunc("POST /api/conversations", s.handleNew)
	mux.HandleFunc("GET /api/conversations/{id}", s.handleGet)
	mux.HandleFunc("PATCH /api/conversations/{id}", s.handleRename)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDelete)
	mux.HandleFunc("POST /api/conversations/{id}/turns", s.handleTurn)
	mux.HandleFunc("POST /api/conversations/{id}/again", s.handleAgain)
}

// guard пропускает только запросы к себе и только со своей страницы.
func guard(port int, next http.Handler) http.Handler {
	hosts := map[string]bool{}
	origins := map[string]bool{}
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]"} {
		hp := fmt.Sprintf("%s:%d", h, port)
		hosts[hp] = true
		origins["http://"+hp] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Host — против DNS rebinding: чужая страница, переназначившая своё
		// имя на 127.0.0.1, приходит со своим Host.
		if !hosts[r.Host] {
			http.Error(w, "неверный Host", http.StatusMisdirectedRequest)
			return
		}
		// Запрос, меняющий состояние, должен прийти с этой же страницы.
		// Браузер ставит Origin на такие запросы; если какой-то его не
		// поставил, своё происхождение подтверждает Sec-Fetch-Site — его
		// страница подделать не может.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			same := origins[origin] || (origin == "" && r.Header.Get("Sec-Fetch-Site") == "same-origin")
			if !same {
				http.Error(w, "запрос не с этой страницы", http.StatusForbidden)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; "+
			"style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ответ не записан: %v", err)
	}
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// loadConfig читает конфиг с диска на каждый запрос: файл маленький, а
// правка из настроек видна сразу, без перезапуска.
func (s *Server) loadConfig() (config.Config, keys.Store, error) {
	cfg, err := config.Load(s.Paths.Config)
	return cfg, keys.Store{Path: s.Paths.Keys}, err
}

func (s *Server) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return http.DefaultClient
}
