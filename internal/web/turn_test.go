package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

const answer = "Добродетель надвое.\n" +
	">> cite: Nicomachean Ethics II.1\n" +
	">> en: moral virtue comes about as a result of habit\n" +
	">> ru: нравственная добродетель возникает как следствие привычки\n" +
	">> cite: Nicomachean Ethics II.1\n" +
	">> en: virtue is a winged chariot of the gods\n" +
	">> ru: добродетель есть крылатая колесница богов\n" +
	">> вывод: середина берётся относительно нас\n"

var ethics = corpus.Chapter{
	Work: "Nicomachean Ethics", WorkSlug: "nicomachean-ethics", Book: "II", Number: 1,
	Group: "ethics", Authenticity: "genuine", Source: "mit", Cite: "Nicomachean Ethics II.1",
	Text: "Virtue, then, being of two kinds, intellectual and moral, intellectual virtue in the main owes both its birth and its growth to teaching, while moral virtue comes about as a result of habit, whence also its name is formed.",
	Path: "nicomachean-ethics/ii/01.md",
}

// script — ход с поиском: вызов инструмента, потом ответ построчно.
func script() [][]provider.Event {
	var second []provider.Event
	for _, line := range strings.SplitAfter(answer, "\n") {
		if line != "" {
			second = append(second, provider.Event{Kind: provider.EventText, Text: line})
		}
	}
	second = append(second,
		provider.Event{Kind: provider.EventUsage, Usage: provider.Usage{Input: 1200, Output: 300, CacheRead: 8000, Known: true}},
		provider.Event{Kind: provider.EventDone, StopReason: "end_turn"})
	return [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{ID: "t1", Name: "search_corpus", Input: map[string]any{"query": "moral virtue habit"}}},
			{Kind: provider.EventUsage, Usage: provider.Usage{Input: 900, Output: 20, CacheRead: 8000, Known: true}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		second,
	}
}

type sseEvent struct {
	Name string
	Data map[string]any
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, chunk := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(chunk, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				ev.Name = v
			}
			if v, ok := strings.CutPrefix(line, "data: "); ok {
				if err := json.Unmarshal([]byte(v), &ev.Data); err != nil {
					t.Fatalf("data не JSON: %q", v)
				}
			}
		}
		out = append(out, ev)
	}
	return out
}

const convID = "2026-11-18T10-00-00"

func TestTurnStreamsBlocksAndJournals(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: script()}, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"Что такое добродетель?"}`)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
	var names []string
	var quotes []map[string]any
	var usage map[string]any
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "delta" {
			continue
		}
		names = append(names, ev.Name)
		if ev.Name == "thinking" && len(ev.Data) != 0 {
			t.Fatalf("thinking несёт данные: %v", ev.Data)
		}
		if ev.Name == "block" && ev.Data["kind"] == "quote" {
			quotes = append(quotes, ev.Data)
		}
		if ev.Name == "usage" {
			usage = ev.Data["usage"].(map[string]any)
		}
	}
	// thinking — перед каждым запросом к модели: до поиска и после него.
	want := "thinking search thinking block block block block usage done"
	if strings.Join(names, " ") != want {
		t.Fatalf("события %v, ожидалось %s", names, want)
	}
	if quotes[0]["verified"] != true || quotes[0]["chapter"] != ethics.Path || quotes[1]["verified"] != false {
		t.Fatalf("врезки %v", quotes)
	}
	if usage["input"] != 2100.0 || usage["output"] != 320.0 || usage["cache_read"] != 16000.0 {
		t.Fatalf("расход %v", usage)
	}

	recs, err := chat.History{Path: s.conv(convID).history.Path}.Load()
	if err != nil || len(recs) != 2 || recs[0].Role != "user" || recs[1].Role != "assistant" {
		t.Fatalf("журнал %+v, %v", recs, err)
	}
	a := recs[1]
	if len(a.Quotes) != 2 || !a.Quotes[0].Verified || a.Quotes[1].Verified || a.Usage == nil || a.StopReason != "end" {
		t.Fatalf("запись ответа %+v", a)
	}
}

func TestParallelTurnIsRejected(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: script()}, fakeFinder{})
	c := s.conv(convID)
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"х"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("код %d, ожидался 409", rec.Code)
	}
}

// blockingProvider отдаёт начало ответа и ждёт, пока клиент не уйдёт.
type blockingProvider struct{}

func (blockingProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	return &blockingStream{ctx: ctx}, nil
}

type blockingStream struct {
	ctx  context.Context
	sent bool
	cur  provider.Event
}

func (s *blockingStream) Next() bool {
	if !s.sent {
		s.sent = true
		s.cur = provider.Event{Kind: provider.EventText, Text: "Начало ответа"}
		return true
	}
	<-s.ctx.Done()
	return false
}
func (s *blockingStream) Event() provider.Event { return s.cur }
func (s *blockingStream) Err() error            { return s.ctx.Err() }
func (s *blockingStream) Close() error          { return nil }

func TestCancelledTurnKeepsPartialAnswer(t *testing.T) {
	s := newTestServer(t, blockingProvider{}, fakeFinder{})
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+"/turns",
		strings.NewReader(`{"text":"вопрос"}`)).WithContext(ctx)
	req.Host = "127.0.0.1:8337"
	req.Header.Set("Origin", "http://127.0.0.1:8337")
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)

	recs, _ := chat.History{Path: s.conv(convID).history.Path}.Load()
	last := recs[len(recs)-1]
	if last.Role != "assistant" || last.Text != "Начало ответа" || last.StopReason != "cancelled" {
		t.Fatalf("последняя запись %+v", last)
	}
	if n := len(s.conv(convID).session.Messages); n != 2 {
		t.Fatalf("в сессии %d сообщений, ожидалось 2 (вопрос и начало ответа)", n)
	}
}

func TestAgainRetractsAndAsksAgain(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{{Kind: provider.EventText, Text: "первый ответ\n"}, {Kind: provider.EventDone, StopReason: "end_turn"}},
		{{Kind: provider.EventText, Text: "второй ответ\n"}, {Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	s := newTestServer(t, p, fakeFinder{})
	h := s.Handler()
	do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/again", `{}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	view := do(t, h, http.MethodGet, "/api/conversations/"+convID, "")
	var got struct {
		Items []item `json:"items"`
	}
	json.Unmarshal(view.Body.Bytes(), &got)
	if len(got.Items) != 2 || got.Items[0].Text != "вопрос" || got.Items[1].Blocks[0].Text != "второй ответ" {
		t.Fatalf("беседа после «ещё раз»: %+v", got.Items)
	}
	if msgs := s.conv(convID).session.Messages; len(msgs) != 2 || msgs[1].Text != "второй ответ\n" {
		t.Fatalf("сессия %+v", msgs)
	}
}

// writeConfig кладёт в config.json сервера присланный текст как есть.
func writeConfig(t *testing.T, s *Server, body string) {
	t.Helper()
	if err := os.WriteFile(s.Paths.Config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// okTurn — ответ без поиска, одной строкой.
func okTurn(text string) []provider.Event {
	return []provider.Event{
		{Kind: provider.EventText, Text: text},
		{Kind: provider.EventUsage, Usage: provider.Usage{Input: 100, Output: 10, Known: true}},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}
}

func TestOverBudgetWarnsButStillAnswers(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, fakeFinder{})
	writeConfig(t, s, `{"context_budget":1}`)
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	var warnings, names []string
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "warning" {
			warnings = append(warnings, ev.Data["text"].(string))
		}
		if ev.Name != "delta" {
			names = append(names, ev.Name)
		}
	}
	if len(warnings) != 1 || warnings[0] != "бюджет превышен и после урезания" {
		t.Fatalf("предупреждения %q", warnings)
	}
	if got := strings.Join(names, " "); got != "thinking block warning usage done" {
		t.Fatalf("события %q: запрос должен уйти и получить ответ, а не быть заблокирован", got)
	}
}

func TestFailedTurnJournalsAndStreamsUsageAsUnknown(t *testing.T) {
	// Первый запрос хода прошёл и сообщил расход, второй (после поиска)
	// падает: сценарий кончился. Набранная сумма остаётся, но точной не
	// выдаётся — ни в журнале, ни в потоке.
	s := newTestServer(t, &scriptedProvider{turns: script()[:1]}, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)

	var names []string
	var usageEv map[string]any
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "delta" {
			continue
		}
		names = append(names, ev.Name)
		if ev.Name == "usage" {
			usageEv = ev.Data
		}
	}
	if got := strings.Join(names, " "); got != "thinking search thinking usage error" {
		t.Fatalf("события %q", got)
	}
	u := usageEv["usage"].(map[string]any)
	if u["known"] != false || u["input"] != 900.0 || u["output"] != 20.0 || u["cache_read"] != 8000.0 {
		t.Fatalf("расход в потоке %v: числа завершённых запросов остаются, known — false", u)
	}
	if usageEv["conversation_tokens"] != 0.0 {
		t.Fatalf("неточный расход попал в сумму беседы: %v", usageEv)
	}

	recs, _ := chat.History{Path: s.conv(convID).history.Path}.Load()
	a := recs[len(recs)-1]
	if a.StopReason != "error" || a.Usage == nil || a.Usage.Known || a.Usage.Input != 900 {
		t.Fatalf("запись ответа %+v, usage %+v", a, a.Usage)
	}
}

func TestCancelledTurnJournalsUsageAsUnknown(t *testing.T) {
	s := newTestServer(t, blockingProvider{}, fakeFinder{})
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+"/turns",
		strings.NewReader(`{"text":"вопрос"}`)).WithContext(ctx)
	req.Host = "127.0.0.1:8337"
	req.Header.Set("Origin", "http://127.0.0.1:8337")
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)

	recs, _ := chat.History{Path: s.conv(convID).history.Path}.Load()
	a := recs[len(recs)-1]
	if a.StopReason != "cancelled" || a.Usage == nil || a.Usage.Known {
		t.Fatalf("запись ответа %+v, usage %+v: расход остановленного хода неизвестен", a, a.Usage)
	}
}

func TestAgainAfterFailedTurnKeepsEarlierTurn(t *testing.T) {
	// Неудавшийся ход Turn из сессии откатил, поэтому «ещё раз» не должно
	// выбрасывать из неё предыдущий, удавшийся ход.
	p := &scriptedProvider{turns: [][]provider.Event{okTurn("первый ответ\n")}}
	s := newTestServer(t, p, fakeFinder{})
	h := s.Handler()
	do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"первый вопрос"}`)
	do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"второй вопрос"}`) // сценарий кончился
	p.mu.Lock()
	p.turns = append(p.turns, okTurn("ответ после повтора\n"))
	p.mu.Unlock()
	if rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/again", `{}`); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var got []string
	for _, m := range s.conv(convID).session.Messages {
		got = append(got, m.Text)
	}
	want := []string{"первый вопрос", "первый ответ\n", "второй вопрос", "ответ после повтора\n"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("в сессии %q, ожидалось %q", got, want)
	}
}

func TestAgainRefusedBeforeRetractKeepsQuestion(t *testing.T) {
	// Отказ до начала хода (ключ пропал) не должен прятать вопрос: отзыв
	// записывается, только когда ход действительно начнётся.
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, fakeFinder{})
	h := s.Handler()
	do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	noKey(t, s)
	if rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/again", `{}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), errNoKey.Error()) {
		t.Fatalf("код %d, ожидался 400 с текстом об отсутствии ключа: %s", rec.Code, rec.Body.String())
	}
	recs, _ := chat.History{Path: s.conv(convID).history.Path}.Load()
	for _, r := range recs {
		if r.Role == chat.RoleRetract {
			t.Fatal("отзыв записан, хотя ход не начался")
		}
	}
	if its := items(recs); len(its) != 2 || its[0].Text != "вопрос" {
		t.Fatalf("беседа после отказа: %+v", its)
	}
	if n := len(s.conv(convID).session.Messages); n != 2 {
		t.Fatalf("в сессии %d сообщений, ожидалось 2: отказ не должен трогать сессию", n)
	}
}

func TestAgainOnSessionRestoredFromJournal(t *testing.T) {
	// После перезапуска сервера сессии нет: она соберётся из журнала, где
	// отзыв уже записан, и второй раз последний ход отбрасывать нельзя.
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{okTurn("новый ответ\n")}}, fakeFinder{})
	writeJournal(t, s.Paths.History, convID,
		chat.Record{Role: "user", Text: "первый вопрос"},
		chat.Record{Role: "assistant", Text: "первый ответ\n", StopReason: "end"},
		chat.Record{Role: "user", Text: "второй вопрос"},
		chat.Record{Role: "assistant", Text: "второй ответ\n", StopReason: "end"})
	if rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/again", `{}`); rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var got []string
	for _, m := range s.conv(convID).session.Messages {
		got = append(got, m.Text)
	}
	want := []string{"первый вопрос", "первый ответ\n", "второй вопрос", "новый ответ\n"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("в сессии %q, ожидалось %q", got, want)
	}
}

func TestRequestsDuringRunningTurn(t *testing.T) {
	// Пока идёт ход, беседу можно читать и переименовывать, а второй ход и
	// удаление получают 409. Тест под -race: журнал дописывается из двух
	// мест, замок хода и lastPrompt читаются из соседних запросов.
	s := newTestServer(t, blockingProvider{}, fakeFinder{})
	h := s.Handler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+"/turns",
		strings.NewReader(`{"text":"вопрос"}`)).WithContext(ctx)
	req.Host = "127.0.0.1:8337"
	req.Header.Set("Origin", "http://127.0.0.1:8337")
	finished := make(chan struct{})
	go func() { defer close(finished); h.ServeHTTP(httptest.NewRecorder(), req) }()

	// Вопрос в журнале — значит, замок хода уже взят.
	c := s.conv(convID)
	for i := 0; ; i++ {
		if recs, _ := c.history.Load(); len(recs) > 0 {
			break
		}
		if i > 200 {
			t.Fatal("ход не начался")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if rec := do(t, h, http.MethodGet, "/api/conversations/"+convID, ""); rec.Code != 200 {
		t.Fatalf("чтение во время хода: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/api/conversations", ""); rec.Code != 200 {
		t.Fatalf("список во время хода: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPatch, "/api/conversations/"+convID, `{"title":"Во время хода"}`); rec.Code != 200 {
		t.Fatalf("переименование во время хода: %d", rec.Code)
	}
	for _, path := range []string{"/turns", "/again"} {
		if rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+path, `{"text":"ещё"}`); rec.Code != http.StatusConflict {
			t.Fatalf("%s во время хода: %d, ожидался 409", path, rec.Code)
		}
	}
	if rec := do(t, h, http.MethodDelete, "/api/conversations/"+convID, ""); rec.Code != http.StatusConflict {
		t.Fatalf("удаление во время хода: %d, ожидался 409", rec.Code)
	}

	cancel()
	<-finished
	recs, _ := c.history.Load()
	got := map[string]bool{}
	for _, r := range recs {
		got[r.Role] = true
	}
	if len(recs) != 3 || !got["user"] || !got[chat.RoleMeta] || !got["assistant"] {
		t.Fatalf("журнал %+v: ожидались вопрос, переименование и остановленный ответ", recs)
	}
}

// planProvider отвечает на запросы по заранее заданному плану исходов: ok,
// empty (ответ без текста), blank (ответ из одного перевода строки), fail
// (отказ провайдера), fail-partial (обрыв потока после начала ответа),
// cancel-empty, cancel-blank и cancel-partial (клиент уходит до первого слова,
// после одной дельты «\n» и после начала ответа). Остановку делает сам провайдер,
// вызывая cancel, — без пауз и гонок. Сообщения каждого запроса копируются:
// сессия потом перезаписывает свой срез.
type planProvider struct {
	plan     []string
	n        int
	cancel   context.CancelFunc // отмена запроса, который обслуживается сейчас
	requests [][]provider.Message
}

func (p *planProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	p.requests = append(p.requests, append([]provider.Message(nil), req.Messages...))
	outcome := p.plan[p.n]
	p.n++
	switch outcome {
	case "ok":
		return &scriptedStream{events: okTurn(fmt.Sprintf("ответ %d\n", p.n))}, nil
	case "empty":
		return &scriptedStream{events: []provider.Event{{Kind: provider.EventDone, StopReason: "end_turn"}}}, nil
	case "blank":
		return &scriptedStream{events: []provider.Event{
			{Kind: provider.EventText, Text: "\n"},
			{Kind: provider.EventDone, StopReason: "end_turn"}}}, nil
	case "fail":
		return nil, errors.New("сбой провайдера")
	case "fail-partial":
		return &cancelStream{ctx: ctx, partial: "начало ответа", fail: errors.New("обрыв потока")}, nil
	case "cancel-empty":
		return &cancelStream{ctx: ctx, cancel: p.cancel}, nil
	case "cancel-partial":
		return &cancelStream{ctx: ctx, cancel: p.cancel, partial: "начало ответа"}, nil
	case "cancel-blank":
		return &cancelStream{ctx: ctx, cancel: p.cancel, partial: "\n"}, nil
	}
	return nil, fmt.Errorf("неизвестный исход %q", outcome)
}

// cancelStream отдаёт начало ответа (если есть) и обрывается: отменой запроса
// или, если задан fail, ошибкой потока.
type cancelStream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	partial string
	fail    error
	sent    bool
	cur     provider.Event
}

func (s *cancelStream) Next() bool {
	if s.partial != "" && !s.sent {
		s.sent = true
		s.cur = provider.Event{Kind: provider.EventText, Text: s.partial}
		return true
	}
	if s.cancel != nil {
		s.cancel()
	}
	return false
}
func (s *cancelStream) Event() provider.Event { return s.cur }
func (s *cancelStream) Err() error {
	if s.fail != nil {
		return s.fail
	}
	return s.ctx.Err()
}
func (s *cancelStream) Close() error { return nil }

type turnStep struct {
	again   bool
	outcome string
}

func flatMessages(msgs []provider.Message) string {
	parts := make([]string, len(msgs))
	for i, m := range msgs {
		parts[i] = string(m.Role) + ":" + strings.TrimSpace(m.Text)
	}
	return strings.Join(parts, " | ")
}

// noBlank — в истории нет сообщения из одних пробельных символов (без вызовов
// инструмента и их результатов): Anthropic отвергает такой текстовый блок.
func noBlank(msgs []provider.Message) bool {
	for _, m := range msgs {
		if strings.TrimSpace(m.Text) == "" && len(m.ToolCalls) == 0 && len(m.ToolResults) == 0 {
			return false
		}
	}
	return true
}

// alternates — сообщения чередуются user, assistant и начинаются с user.
func alternates(msgs []provider.Message) bool {
	for i, m := range msgs {
		want := provider.RoleUser
		if i%2 == 1 {
			want = provider.RoleAssistant
		}
		if m.Role != want {
			return false
		}
	}
	return true
}

// TestRestartRestoresTheLiveSession: что бы ни случилось в беседе — ошибка,
// остановка до слова или посреди ответа, пустой ответ, «ещё раз» после любого
// из них, — сессия, собранная из журнала новым сервером, обязана совпасть с
// живой перед перезапуском. Иначе после перезапуска модель видит другую
// беседу, а лишний вопрос подряд часть провайдеров отвергает.
func TestRestartRestoresTheLiveSession(t *testing.T) {
	ok, again := "ok", true
	sequences := map[string][]turnStep{
		"ok, остановка до слова, ok":                 {{false, ok}, {false, "cancel-empty"}, {false, ok}},
		"ok, ошибка, ещё раз":                        {{false, ok}, {false, "fail"}, {again, ok}},
		"ok, остановка с текстом, ещё раз, ok":       {{false, ok}, {false, "cancel-partial"}, {again, ok}, {false, ok}},
		"ещё раз сразу после ok":                     {{false, ok}, {again, ok}},
		"остановка до слова, ok":                     {{false, "cancel-empty"}, {false, ok}},
		"ошибка, остановка с текстом, ok":            {{false, "fail"}, {false, "cancel-partial"}, {false, ok}},
		"ok, пустой ответ, ok":                       {{false, ok}, {false, "empty"}, {false, ok}},
		"ok, ok, ещё раз остановлен, ещё раз":        {{false, ok}, {false, ok}, {again, "cancel-empty"}, {again, ok}},
		"ok, ещё раз с ошибкой, новый вопрос":        {{false, ok}, {again, "fail"}, {false, ok}},
		"ещё раз два раза подряд":                    {{false, ok}, {again, ok}, {again, ok}},
		"ошибка, ещё раз с остановкой, ещё раз":      {{false, ok}, {false, "fail"}, {again, "cancel-partial"}, {again, ok}},
		"пустой ответ, ещё раз":                      {{false, ok}, {false, "empty"}, {again, ok}},
		"ok, ещё раз с остановкой с текстом, ok, ok": {{false, ok}, {again, "cancel-partial"}, {false, ok}, {false, ok}},
		"ok, остановка с текстом, новый вопрос":      {{false, ok}, {false, "cancel-partial"}, {false, ok}},
		"ok, остановка с текстом, ещё раз":           {{false, ok}, {false, "cancel-partial"}, {again, ok}},
		"остановка с текстом, ещё раз, ok":           {{false, "cancel-partial"}, {again, ok}, {false, ok}},
		"ошибка с началом ответа, ok":                {{false, "fail-partial"}, {false, ok}},
		"ok, ошибка с началом ответа, ok":            {{false, ok}, {false, "fail-partial"}, {false, ok}},
		"ok, ошибка с началом ответа, ещё раз":       {{false, ok}, {false, "fail-partial"}, {again, ok}},
		"ok, остановка до слова, ещё раз":            {{false, ok}, {false, "cancel-empty"}, {again, ok}},
		"две остановки с текстом подряд, ok":         {{false, "cancel-partial"}, {false, "cancel-partial"}, {false, ok}},
		"ok, остановка после пробела, ok":            {{false, ok}, {false, "cancel-blank"}, {false, ok}},
		"остановка после пробела, ещё раз":           {{false, "cancel-blank"}, {again, ok}},
		"ok, пробельный ответ, ok":                   {{false, ok}, {false, "blank"}, {false, ok}},
		"ok, пробельный ответ, ещё раз":              {{false, ok}, {false, "blank"}, {again, ok}},
		"пробел при остановке, потом текст":          {{false, "cancel-blank"}, {false, "cancel-partial"}, {false, ok}},
	}
	for name, steps := range sequences {
		t.Run(name, func(t *testing.T) {
			plan := make([]string, len(steps))
			for i, st := range steps {
				plan[i] = st.outcome
			}
			p := &planProvider{plan: plan}
			s := newTestServer(t, p, fakeFinder{})
			h := s.Handler()
			for i, st := range steps {
				ctx, cancel := context.WithCancel(context.Background())
				p.cancel = cancel
				path, body := "/turns", fmt.Sprintf(`{"text":"вопрос %d"}`, i+1)
				if st.again {
					path, body = "/again", `{}`
				}
				req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+path,
					strings.NewReader(body)).WithContext(ctx)
				req.Host = "127.0.0.1:8337"
				req.Header.Set("Origin", "http://127.0.0.1:8337")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				cancel()
				if rec.Code != 200 {
					t.Fatalf("шаг %d (%s): код %d: %s", i+1, path, rec.Code, rec.Body.String())
				}
			}
			live := s.conv(convID).session.Messages

			// Каждый шаг дошёл до своего исхода, а не до какого-то соседнего.
			reasons := map[string]string{"ok": "end", "empty": "end", "blank": "end", "fail": "error",
				"fail-partial": "error", "cancel-empty": "cancelled", "cancel-blank": "cancelled", "cancel-partial": "cancelled"}
			var wantReasons, gotReasons []string
			for _, st := range steps {
				wantReasons = append(wantReasons, reasons[st.outcome])
			}
			recs, _ := s.conv(convID).history.Load()
			for _, r := range recs {
				if r.Role == "assistant" {
					gotReasons = append(gotReasons, r.StopReason)
				}
			}
			if strings.Join(gotReasons, " ") != strings.Join(wantReasons, " ") {
				t.Fatalf("причины остановки в журнале %v, ожидались %v", gotReasons, wantReasons)
			}

			// Перезапуск: новый сервер на тех же каталогах, сессия — из журнала.
			s2 := New(s.Root, s.Paths, 8337)
			s2.System, s2.NewProvider, s2.Finder = s.System, s.NewProvider, s.Finder
			sess, err := s2.session(s2.conv(convID), app.Connection{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := flatMessages(sess.Messages), flatMessages(live); got != want {
				t.Fatalf("после перезапуска:\n  %s\nживая сессия:\n  %s", got, want)
			}
			if !alternates(live) || !alternates(sess.Messages) {
				t.Fatalf("роли не чередуются: живая %s, восстановленная %s", flatMessages(live), flatMessages(sess.Messages))
			}
			// Совпадения мало: обе стороны могли бы одинаково оставить пробельный ответ.
			if !noBlank(live) || !noBlank(sess.Messages) {
				t.Fatalf("в истории есть сообщение из одних пробелов: живая %q, восстановленная %q", flatMessages(live), flatMessages(sess.Messages))
			}
		})
	}
}

// Остановка посреди ответа: следующий вопрос («продолжай») уходит модели вместе
// с остановленным вопросом и тем, что она успела сказать, — и до перезапуска,
// и после него. Это то, ради чего остановленный ход остаётся в истории.
func TestFollowUpAfterStopSeesStoppedAnswer(t *testing.T) {
	p := &planProvider{plan: []string{"ok", "cancel-partial", "ok"}}
	s := newTestServer(t, p, fakeFinder{})
	h := s.Handler()
	for i, outcome := range p.plan {
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+"/turns",
			strings.NewReader(fmt.Sprintf(`{"text":"вопрос %d"}`, i+1))).WithContext(ctx)
		req.Host = "127.0.0.1:8337"
		req.Header.Set("Origin", "http://127.0.0.1:8337")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		cancel()
		if rec.Code != 200 {
			t.Fatalf("шаг %d (%s): код %d", i+1, outcome, rec.Code)
		}
	}
	want := "user:вопрос 1 | assistant:ответ 1 | user:вопрос 2 | assistant:начало ответа | user:вопрос 3"
	if got := flatMessages(p.requests[2]); got != want {
		t.Fatalf("запрос после остановки:\n  %s\nожидалось:\n  %s", got, want)
	}

	// После перезапуска следующий запрос видит ту же историю.
	p2 := &planProvider{plan: []string{"ok"}}
	s2 := New(s.Root, s.Paths, 8337)
	s2.System, s2.Finder = s.System, s.Finder
	s2.NewProvider = func(app.Connection) (provider.Provider, error) { return p2, nil }
	if rec := do(t, s2.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос 4"}`); rec.Code != 200 {
		t.Fatalf("после перезапуска: код %d", rec.Code)
	}
	want = "user:вопрос 1 | assistant:ответ 1 | user:вопрос 2 | assistant:начало ответа | user:вопрос 3 | assistant:ответ 3 | user:вопрос 4"
	if got := flatMessages(p2.requests[0]); got != want {
		t.Fatalf("запрос после перезапуска:\n  %s\nожидалось:\n  %s", got, want)
	}
}

// Ответ из одних пробельных символов — пустой: и после Stop сразу за дельтой
// «\n», и когда провайдер сам закончил таким ответом. Anthropic отвергает
// текстовый блок без видимого текста, поэтому ход остаётся в журнале и на
// экране, но не в истории для модели — ни в живой сессии, ни после перезапуска.
// Следующий запрос провайдеру не содержит ни вопроса, ни пробельного ответа.
func TestWhitespaceOnlyAnswerIsNotInHistory(t *testing.T) {
	for _, outcome := range []string{"cancel-blank", "blank"} {
		t.Run(outcome, func(t *testing.T) {
			p := &planProvider{plan: []string{"ok", outcome, "ok"}}
			s := newTestServer(t, p, fakeFinder{})
			h := s.Handler()
			for i, o := range p.plan {
				ctx, cancel := context.WithCancel(context.Background())
				p.cancel = cancel
				req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+convID+"/turns",
					strings.NewReader(fmt.Sprintf(`{"text":"вопрос %d"}`, i+1))).WithContext(ctx)
				req.Host = "127.0.0.1:8337"
				req.Header.Set("Origin", "http://127.0.0.1:8337")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				cancel()
				if rec.Code != 200 {
					t.Fatalf("шаг %d (%s): код %d", i+1, o, rec.Code)
				}
			}

			// Третий запрос ушёл без второго хода.
			want := "user:вопрос 1 | assistant:ответ 1 | user:вопрос 3"
			if got := flatMessages(p.requests[2]); got != want {
				t.Fatalf("запрос после пробельного ответа:\n  %s\nожидалось:\n  %s", got, want)
			}
			if !noBlank(p.requests[2]) {
				t.Fatal("в запросе провайдеру есть сообщение из одних пробелов")
			}
			live := s.conv(convID).session.Messages
			want = "user:вопрос 1 | assistant:ответ 1 | user:вопрос 3 | assistant:ответ 3"
			if got := flatMessages(live); got != want {
				t.Fatalf("живая сессия:\n  %s\nожидалось:\n  %s", got, want)
			}

			// В журнале ход остался: вопрос и пробельный ответ с причиной остановки.
			recs, _ := s.conv(convID).history.Load()
			var blank []chat.Record
			for _, r := range recs {
				if r.Role == "assistant" && strings.TrimSpace(r.Text) == "" {
					blank = append(blank, r)
				}
			}
			wantReason := map[string]string{"cancel-blank": "cancelled", "blank": "end"}[outcome]
			if len(blank) != 1 || blank[0].Text != "\n" || blank[0].StopReason != wantReason {
				t.Fatalf("в журнале пробельных ответов %+v, ожидался один с текстом \\n и причиной %s", blank, wantReason)
			}

			// После перезапуска следующий запрос видит ту же историю.
			p2 := &planProvider{plan: []string{"ok"}}
			s2 := New(s.Root, s.Paths, 8337)
			s2.System, s2.Finder = s.System, s.Finder
			s2.NewProvider = func(app.Connection) (provider.Provider, error) { return p2, nil }
			if rec := do(t, s2.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос 4"}`); rec.Code != 200 {
				t.Fatalf("после перезапуска: код %d", rec.Code)
			}
			want = "user:вопрос 1 | assistant:ответ 1 | user:вопрос 3 | assistant:ответ 3 | user:вопрос 4"
			if got := flatMessages(p2.requests[0]); got != want {
				t.Fatalf("запрос после перезапуска:\n  %s\nожидалось:\n  %s", got, want)
			}
			if !noBlank(p2.requests[0]) {
				t.Fatal("после перезапуска в запросе есть сообщение из одних пробелов")
			}
		})
	}
}

// Испорченный keys.json: ход отказывает
// фиксированным текстом — без пути к файлу и без кусков разбора (там есть знак
// из самого файла). Ход не начинается: журнал остаётся как был, а «ещё раз» не
// отзывает прежний ответ.
func TestTurnWithCorruptKeysFileShowsNoPathOrParserText(t *testing.T) {
	s := corruptKeysServer(t)
	s.NewProvider = func(app.Connection) (provider.Provider, error) {
		t.Fatal("провайдер создан при испорченном keys.json")
		return nil, nil
	}
	writeJournal(t, s.Paths.History, convID,
		chat.Record{Role: "user", Text: "прежний вопрос"}, chat.Record{Role: "assistant", Text: "прежний ответ\n"})
	journal := filepath.Join(s.Paths.History, convID+".jsonl")
	before, _ := os.ReadFile(journal)
	h := s.Handler()
	for _, path := range []string{"/turns", "/again"} {
		rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+path, `{"text":"вопрос"}`)
		msg := rec.Body.String()
		if rec.Code != http.StatusBadRequest || !strings.Contains(msg, "keys.json не читается") || !cyrillicRe.MatchString(msg) {
			t.Errorf("%s: код %d: %s", path, rec.Code, msg)
		}
		for _, leak := range []string{s.Paths.Keys, filepath.Dir(s.Paths.Keys), "invalid character", "ПОЛОМКА", "looking for", "sk-ant"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s: в ответе есть %q: %s", path, leak, msg)
			}
		}
	}
	if after, _ := os.ReadFile(journal); !bytes.Equal(before, after) {
		t.Errorf("отказ изменил журнал беседы: %q", after)
	}
}

// captureProvider запоминает запросы к модели: по ним видно системную добавку.
type captureProvider struct{ reqs []provider.Request }

func (p *captureProvider) Stream(_ context.Context, req provider.Request) (provider.Stream, error) {
	p.reqs = append(p.reqs, req)
	return &scriptedStream{events: []provider.Event{
		{Kind: provider.EventText, Text: "Ответ.\n"},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}, nil
}

func TestTurnRussianSelectsCorpusAndPrompt(t *testing.T) {
	p := &captureProvider{}
	s := newTestServer(t, p, fakeFinder{})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns?lang=ru", `{"text":"Что такое добродетель?"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if len(p.reqs) != 1 || !strings.Contains(p.reqs[0].System, "Русский корпус") ||
		!strings.Contains(p.reqs[0].System, "по-русски") {
		t.Fatalf("в системном промпте нет русской добавки: %+v", p.reqs)
	}

	// Английский ход после русского не должен тащить добавку дальше.
	p.reqs = nil
	rec = do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"And virtue?"}`)
	if rec.Code != 200 || len(p.reqs) != 1 || strings.Contains(p.reqs[0].System, "Русский корпус") {
		t.Fatalf("добавка осталась в английском ходе: %d %+v", rec.Code, p.reqs)
	}
}

func TestPrepareSessionSwitchesCollectionBackAndForth(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.Finder = nil
	c := &conv{baseSystem: "ПЕРСОНА"}
	sess := &chat.Session{}

	s.prepareSession(sess, c, langRu)
	ru, ok := sess.Corpus.(*corpus.Searcher)
	if !ok || ru.Collection != "aristotle-ru" || filepath.Base(ru.Root) != "corpus-ru" {
		t.Fatalf("русский поиск: %+v", sess.Corpus)
	}
	if !strings.Contains(sess.System, "Русский корпус") {
		t.Fatalf("нет добавки: %q", sess.System)
	}

	s.prepareSession(sess, c, langEn)
	en, ok := sess.Corpus.(*corpus.Searcher)
	if !ok || en.Collection != "aristotle" || filepath.Base(en.Root) != "corpus" {
		t.Fatalf("английский поиск: %+v", sess.Corpus)
	}
	if sess.System != "ПЕРСОНА" {
		t.Fatalf("добавка накопилась: %q", sess.System)
	}
}

func TestTurnRejectsUnknownCorpusLang(t *testing.T) {
	p := &captureProvider{}
	s := newTestServer(t, p, fakeFinder{})
	for _, path := range []string{"/turns?lang=de", "/again?lang=de"} {
		rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+path, `{"text":"вопрос"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: код %d", path, rec.Code)
		}
	}
	if len(p.reqs) != 0 {
		t.Fatalf("до модели дошёл запрос при неверном языке: %+v", p.reqs)
	}
}

// Язык врезки едет и в поток, и в журнал: панель источников читает главу из
// того корпуса, откуда она пришла.
func TestRussianQuoteCarriesLang(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: script()}, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns?lang=ru", `{"text":"Что такое добродетель?"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "block" && ev.Data["kind"] == "quote" {
			found = true
			if ev.Data["lang"] != "ru" {
				t.Fatalf("у врезки нет lang=ru: %v", ev.Data)
			}
		}
	}
	if !found {
		t.Fatal("в потоке нет врезки")
	}
	recs, err := (&chat.History{Path: filepath.Join(s.Paths.History, convID+".jsonl")}).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Role == "assistant" && len(r.Quotes) > 0 {
			if r.Quotes[0].Lang != "ru" {
				t.Fatalf("в журнале lang=%q", r.Quotes[0].Lang)
			}
			return
		}
	}
	t.Fatal("в журнале нет ответа с врезкой")
}

func TestConnectionAllowsNoKeyForOpenCodeCLI(t *testing.T) {
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	data := `{"base_url":"opencode-cli","model":"beam-free","reasoning_effort":"high","context_budget":1000}`
	if err := os.WriteFile(s.Paths.Config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	conn, err := s.connection()
	if err != nil {
		t.Fatalf("opencode-cli без ключа: %v", err)
	}
	if conn.BaseURL != "opencode-cli" || conn.Model != "beam-free" {
		t.Fatalf("подключение: %+v", conn)
	}
}

func TestCheckKeyForOpenCodeCLIIsNoop(t *testing.T) {
	s := newTestServer(t, nil, nil)
	noKey(t, s)
	data := `{"base_url":"opencode-cli","model":"beam-free","reasoning_effort":"high","context_budget":1000}`
	if err := os.WriteFile(s.Paths.Config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s.Handler(), http.MethodPost, "/api/key/check", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "ключ не нужен") {
		t.Fatalf("проверка ключа для CLI: %d %s", rec.Code, rec.Body.String())
	}
}
