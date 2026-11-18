package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

// idRe — id беседы: время начала из имени журнала (app.HistoryLayout),
// с номером на случай двух бесед в одну секунду. Ничего, кроме цифр и
// дефисов, — путь из id не выведет за каталог журналов.
var idRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}(-\d+)?$`)

type conv struct {
	id      string
	history chat.History
	mu      sync.Mutex // один ход за раз; занят — 409
	session *chat.Session
	// lastPrompt — вход последнего запроса, для шкалы контекста; читается
	// без блокировки хода.
	lastPrompt atomic.Int64
}

func (s *Server) conv(id string) *conv {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.convs[id]
	if !ok {
		c = &conv{id: id, history: chat.History{Path: filepath.Join(s.Paths.History, id+".jsonl")}}
		s.convs[id] = c
	}
	return c
}

func (s *Server) convFromPath(w http.ResponseWriter, r *http.Request) (*conv, bool) {
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		http.Error(w, "неверный id беседы", http.StatusBadRequest)
		return nil, false
	}
	return s.conv(id), true
}

type convSummary struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
	Tokens  int       `json:"tokens"`
}

func summarize(id string, recs []chat.Record) convSummary {
	sum := convSummary{ID: id, Title: chat.TitleOf(recs)}
	if sum.Title == "" {
		sum.Title = "Новая беседа"
	}
	for _, r := range recs {
		if r.Time.After(sum.Updated) {
			sum.Updated = r.Time
		}
		if r.Role != "assistant" {
			continue
		}
		if r.Usage != nil && r.Usage.Known {
			sum.Tokens += r.Usage.Total()
		}
	}
	return sum
}

type item struct {
	Type          string          `json:"type"` // user, assistant, trim
	Text          string          `json:"text,omitempty"`
	Blocks        []blockView     `json:"blocks,omitempty"`
	Usage         *provider.Usage `json:"usage,omitempty"`
	Model         string          `json:"model,omitempty"`
	StopReason    string          `json:"stop_reason,omitempty"`
	QuotesUnknown bool            `json:"quotes_unknown,omitempty"`
	Excerpts      int             `json:"excerpts,omitempty"`
	Turns         int             `json:"turns,omitempty"`
}

// blocksOf разбирает ответ из журнала заново. Главы того хода не сохранены,
// поэтому сверка берётся из записанных quotes по порядку; у ответов до
// появления quotes (журналы командной строки) сверка неизвестна — второй
// результат.
func blocksOf(rec chat.Record) ([]render.Block, bool) {
	p := &render.Parser{}
	a := &render.Assembler{}
	var out []render.Block
	for _, ev := range p.Write(rec.Text) {
		out = append(out, a.Handle(ev, nil)...)
	}
	for _, ev := range p.Flush() {
		out = append(out, a.Handle(ev, nil)...)
	}
	out = append(out, a.Flush()...)
	unknown := false
	qi := 0
	for i := range out {
		if out[i].Kind != render.BlockQuote {
			continue
		}
		if qi < len(rec.Quotes) {
			out[i].Verified = rec.Quotes[qi].Verified
			out[i].Chapter = rec.Quotes[qi].Chapter
			qi++
		} else {
			unknown = true
		}
	}
	return out, unknown
}

// items — беседа для экрана: служебные записи разобраны, отозванное «ещё
// раз» убрано.
func items(recs []chat.Record) []item {
	var out []item
	for _, r := range recs {
		switch r.Role {
		case "user":
			out = append(out, item{Type: "user", Text: r.Text})
		case "assistant":
			blocks, unknown := blocksOf(r)
			out = append(out, item{Type: "assistant", Text: r.Text, Blocks: blocksForWire(blocks),
				Usage: r.Usage, Model: r.Model,
				StopReason: r.StopReason, QuotesUnknown: unknown && r.Quotes == nil})
		case chat.RoleTrim:
			out = append(out, item{Type: "trim", Excerpts: r.Excerpts, Turns: r.Turns})
		case chat.RoleRetract:
			for i := len(out) - 1; i >= 0; i-- {
				if out[i].Type == "user" {
					out = out[:i]
					break
				}
			}
		}
	}
	return out
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.Paths.History)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list := []convSummary{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if e.IsDir() || !ok || !idRe.MatchString(id) {
			continue
		}
		recs, err := s.conv(id).history.Load()
		if err != nil || len(recs) == 0 {
			continue
		}
		list = append(list, summarize(id, recs))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
	writeJSON(w, list)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	recs, err := c.history.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sum := summarize(c.id, recs)
	its := items(recs)
	if its == nil {
		its = []item{}
	}
	writeJSON(w, map[string]any{
		"id": c.id, "title": sum.Title, "items": its,
		"context_used": c.lastPrompt.Load(), "tokens": sum.Tokens,
	})
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	base := time.Now().UTC().Format(app.HistoryLayout)
	s.mu.Lock()
	id := base
	for n := 2; ; n++ {
		_, taken := s.convs[id]
		if _, err := os.Stat(filepath.Join(s.Paths.History, id+".jsonl")); !taken && errors.Is(err, os.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
	s.convs[id] = &conv{id: id, history: chat.History{Path: filepath.Join(s.Paths.History, id+".jsonl")}}
	s.mu.Unlock()
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Title) == "" {
		http.Error(w, "пустое название", http.StatusBadRequest)
		return
	}
	if err := c.history.Append(chat.Record{Role: chat.RoleMeta, Title: chat.CutTitle(in.Title)}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDelete переносит журнал в корзину: удаление беседы — действие
// пользователя, но не повод терять запись насовсем.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	if !c.mu.TryLock() {
		http.Error(w, "в этой беседе идёт ход", http.StatusConflict)
		return
	}
	defer c.mu.Unlock()
	trash := filepath.Join(s.Paths.History, "trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err := os.Rename(c.history.Path, filepath.Join(trash, filepath.Base(c.history.Path)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	delete(s.convs, c.id)
	s.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}
