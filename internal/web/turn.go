package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

type turnIn struct {
	Text    string `json:"text"`
	Profile string `json:"profile"`
}

type usageEvent struct {
	Usage              provider.Usage `json:"usage"`
	CostUSD            *float64       `json:"cost_usd"`
	ContextUsed        int            `json:"context_used"`
	ContextBudget      int            `json:"context_budget"`
	ConversationTokens int            `json:"conversation_tokens"`
	ConversationCost   *float64       `json:"conversation_cost_usd"`
}

func (s *Server) system() (string, error) {
	if s.System != "" {
		return s.System, nil
	}
	return persona.Load(filepath.Join(s.Root, "persona"))
}

func (s *Server) finder() chat.Finder {
	if s.Finder != nil {
		return s.Finder
	}
	return app.NewSearcher(s.Root)
}

func (s *Server) providerFor(p config.Profile) (provider.Provider, error) {
	if s.NewProvider != nil {
		return s.NewProvider(p)
	}
	return app.NewProvider(p)
}

// session — сессия беседы; при первом обращении восстанавливается из
// журнала. Провайдер, бюджет и k ставятся на каждый ход: настройки могли
// поменяться.
func (s *Server) session(c *conv, prof config.Profile, prov provider.Provider, k int) (*chat.Session, error) {
	if c.session == nil {
		system, err := s.system()
		if err != nil {
			return nil, err
		}
		recs, err := c.history.Load()
		if err != nil {
			return nil, err
		}
		c.session = &chat.Session{Corpus: s.finder(), System: system, Messages: app.ResumeMessages(recs)}
	}
	c.session.Provider = prov
	c.session.Budget = prof.ContextBudget
	c.session.DefaultK = k
	return c.session, nil
}

func (s *Server) handleTurn(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	var in turnIn
	if err := readJSON(r, &in); err != nil || strings.TrimSpace(in.Text) == "" {
		http.Error(w, "пустой вопрос", http.StatusBadRequest)
		return
	}
	if !c.mu.TryLock() {
		http.Error(w, "в этой беседе уже идёт ход", http.StatusConflict)
		return
	}
	defer c.mu.Unlock()
	s.runTurn(w, r, c, strings.TrimSpace(in.Text), in.Profile, nil)
}

// handleAgain отзывает последний вопрос с ответом и задаёт вопрос снова.
// Журнал только дописывается, поэтому отзыв — запись retract. Её пишет
// runTurn, когда ход точно начнётся: отказ раньше (нет ключа, нет такого
// профиля) не должен прятать вопрос из беседы.
func (s *Server) handleAgain(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	var in turnIn
	readJSON(r, &in) // тело необязательно: пустое — профиль по умолчанию
	if !c.mu.TryLock() {
		http.Error(w, "в этой беседе уже идёт ход", http.StatusConflict)
		return
	}
	defer c.mu.Unlock()
	recs, err := c.history.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	last := ""
	for _, it := range items(recs) {
		if it.Type == "user" {
			last = it.Text
		}
	}
	if last == "" {
		http.Error(w, "повторять нечего", http.StatusBadRequest)
		return
	}
	// Откатанного хода в сессии нет: снимать с неё нечего, иначе «ещё раз»
	// после сбоя выбросило бы предыдущий, удавшийся ход.
	inSession := app.LastTurnKept(recs)
	s.runTurn(w, r, c, last, in.Profile, func() error {
		if err := c.history.Append(chat.Record{Role: chat.RoleRetract}); err != nil {
			return err
		}
		// Сессия, которой ещё нет, соберётся из журнала уже с отзывом.
		if c.session != nil && inSession {
			c.session.Messages = app.DropLastTurn(c.session.Messages)
		}
		return nil
	})
}

// runTurn ведёт один ход потоком событий. retract (может быть nil) вызывается
// после всех проверок, перед первой записью в журнал.
func (s *Server) runTurn(w http.ResponseWriter, r *http.Request, c *conv, text, profileName string, retract func() error) {
	cfg, store, err := s.loadConfig()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	prof, err := app.Resolve(cfg, store, profileName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prov, err := s.providerFor(prof)
	if err != nil { // чаще всего — нет ключа
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if retract != nil {
		if err := retract(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	sess, err := s.session(c, prof, prov, cfg.CorpusK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, ok := newSSE(w)
	if !ok {
		http.Error(w, "поток не поддерживается", http.StatusInternalServerError)
		return
	}

	journalWarned := false
	journal := func(rec chat.Record) {
		if err := c.history.Append(rec); err != nil && !journalWarned {
			journalWarned = true
			out.send("warning", map[string]string{"text": "журнал беседы не записывается: " + err.Error()})
		}
	}
	sess.OnSearchStart = func(query, group string) {
		out.send("search", map[string]string{"query": query, "group": group})
	}
	sess.OnSearchWarning = func(msg string) {
		out.send("warning", map[string]string{"text": msg})
	}

	journal(chat.Record{Role: "user", Text: text})
	var partial strings.Builder
	var chapters []corpus.Chapter
	var blocks []render.Block
	parser := &render.Parser{}
	asm := &render.Assembler{}
	emit := func(bs []render.Block) {
		for _, b := range bs {
			blocks = append(blocks, b) // в журнал уходит чистый Block
			out.send("block", blockForWire(b))
		}
	}
	baseLen := len(sess.Messages)
	res, err := sess.Turn(r.Context(), text,
		func(delta string) {
			partial.WriteString(delta)
			out.send("delta", map[string]string{"text": delta})
			for _, ev := range parser.Write(delta) {
				emit(asm.Handle(ev, chapters))
			}
		},
		func(cs []corpus.Chapter) { chapters = cs },
	)
	for _, ev := range parser.Flush() {
		emit(asm.Handle(ev, chapters))
	}
	emit(asm.Flush())

	rec := chat.Record{Role: "assistant", Profile: prof.Name, Model: prof.Model, Quotes: app.QuotesOf(blocks)}
	// Расход уходит в журнал и в поток как вернул Turn. После ошибки или
	// остановки он с Known = false: числа завершённых запросов остаются, но
	// точной суммой не считаются и в цену не идут (Cost при Known = false — nil).
	usage := res.Usage
	rec.Usage, rec.CostUSD = &usage, app.Cost(prof.Prices, usage)
	sendUsage := func() convSummary {
		recs, _ := c.history.Load()
		sum := summarize(c.id, recs)
		out.send("usage", usageEvent{Usage: usage, CostUSD: rec.CostUSD,
			ContextUsed: sess.LastPrompt, ContextBudget: sess.Budget,
			ConversationTokens: sum.Tokens, ConversationCost: sum.CostUSD})
		return sum
	}
	if err != nil {
		rec.Text = partial.String()
		if r.Context().Err() != nil {
			// Остановлено пользователем. Turn откатил ход из сессии, а журнал
			// его помнит — возвращаем в сессию вопрос и начало ответа, чтобы
			// следующий ход видел то же, что видит человек (и «продолжай»
			// имело смысл). После перезапуска сессия соберётся из журнала так
			// же (app.AnswerKept); остановка до первого слова или после одних пробелов
			// хода не оставляет.
			rec.StopReason = "cancelled"
			if app.AnswerKept(rec) {
				sess.Messages = append(sess.Messages,
					provider.Message{Role: provider.RoleUser, Text: text},
					provider.Message{Role: provider.RoleAssistant, Text: rec.Text})
			}
			journal(rec)
			return
		}
		rec.StopReason = "error"
		journal(rec)
		sendUsage()
		out.send("error", map[string]string{"message": err.Error()})
		return
	}

	rec.Text, rec.StopReason = res.Text, "end"
	if !app.AnswerKept(rec) {
		// Пустой ответ в историю не идёт: провайдер отвергает сообщение без
		// содержимого, а после перезапуска такого хода там тоже не будет.
		sess.Messages = sess.Messages[:baseLen]
	}
	if res.Trim.Any() {
		journal(chat.Record{Role: chat.RoleTrim, Excerpts: res.Trim.Excerpts, Turns: res.Trim.Turns})
		out.send("trim", res.Trim)
	}
	if res.OverBudget {
		out.send("warning", map[string]string{"text": "бюджет превышен и после урезания"})
	}
	journal(rec)
	c.lastPrompt.Store(int64(sess.LastPrompt))
	sum := sendUsage()
	out.send("done", map[string]string{"stop_reason": "end", "title": sum.Title})
}
