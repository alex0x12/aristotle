package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
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
		if ev.Name == "block" && ev.Data["kind"] == "quote" {
			quotes = append(quotes, ev.Data)
		}
		if ev.Name == "usage" {
			usage = ev.Data["usage"].(map[string]any)
		}
	}
	want := "search block block block block usage done"
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
	if n := len(s.conv(convID).session.Messages); n != 0 {
		t.Fatalf("в сессии %d сообщений, ожидалось 0: остановленный ход откатан целиком, как после перезапуска", n)
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

// writeConfig кладёт в конфиг сервера переопределения профиля по умолчанию.
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
	writeConfig(t, s, `{"profiles":{"deepseek":{"context_budget":1}}}`)
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
	if got := strings.Join(names, " "); got != "block warning usage done" {
		t.Fatalf("события %q: запрос должен уйти и получить ответ, а не быть заблокирован", got)
	}
}

func TestFailedTurnJournalsAndStreamsUsageAsUnknown(t *testing.T) {
	// Первый запрос хода прошёл и сообщил расход, второй (после поиска)
	// падает: сценарий кончился. Набранная сумма остаётся, но точной не
	// выдаётся — ни в журнале, ни в потоке, ни в цене.
	s := newTestServer(t, &scriptedProvider{turns: script()[:1]}, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	writeConfig(t, s, `{"profiles":{"deepseek":{"prices":{"input":1,"output":2,"cache_read":0.5,"cache_write":0}}}}`)
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
	if got := strings.Join(names, " "); got != "search usage error" {
		t.Fatalf("события %q", got)
	}
	u := usageEv["usage"].(map[string]any)
	if u["known"] != false || u["input"] != 900.0 || u["output"] != 20.0 || u["cache_read"] != 8000.0 {
		t.Fatalf("расход в потоке %v: числа завершённых запросов остаются, known — false", u)
	}
	if usageEv["cost_usd"] != nil || usageEv["conversation_tokens"] != 0.0 {
		t.Fatalf("неточный расход попал в цену или сумму беседы: %v", usageEv)
	}

	recs, _ := chat.History{Path: s.conv(convID).history.Path}.Load()
	a := recs[len(recs)-1]
	if a.StopReason != "error" || a.Usage == nil || a.Usage.Known || a.Usage.Input != 900 || a.CostUSD != nil {
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
	if a.StopReason != "cancelled" || a.Usage == nil || a.Usage.Known || a.CostUSD != nil {
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
	// Отказ до начала хода (нет такого профиля) не должен прятать вопрос:
	// отзыв записывается, только когда ход действительно начнётся.
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{okTurn("ответ\n")}}, fakeFinder{})
	h := s.Handler()
	do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/again", `{"profile":"нет-такого"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d, ожидался 400", rec.Code)
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
// empty (ответ без текста), fail (отказ провайдера), cancel-empty и
// cancel-partial (клиент уходит до первого слова и после начала ответа).
// Остановку делает сам провайдер, вызывая cancel, — без пауз и гонок.
type planProvider struct {
	plan   []string
	n      int
	cancel context.CancelFunc // отмена запроса, который обслуживается сейчас
}

func (p *planProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	outcome := p.plan[p.n]
	p.n++
	switch outcome {
	case "ok":
		return &scriptedStream{events: okTurn(fmt.Sprintf("ответ %d\n", p.n))}, nil
	case "empty":
		return &scriptedStream{events: []provider.Event{{Kind: provider.EventDone, StopReason: "end_turn"}}}, nil
	case "fail":
		return nil, errors.New("сбой провайдера")
	case "cancel-empty":
		return &cancelStream{ctx: ctx, cancel: p.cancel}, nil
	case "cancel-partial":
		return &cancelStream{ctx: ctx, cancel: p.cancel, partial: "начало ответа"}, nil
	}
	return nil, fmt.Errorf("неизвестный исход %q", outcome)
}

type cancelStream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	partial string
	sent    bool
	cur     provider.Event
}

func (s *cancelStream) Next() bool {
	if s.partial != "" && !s.sent {
		s.sent = true
		s.cur = provider.Event{Kind: provider.EventText, Text: s.partial}
		return true
	}
	s.cancel()
	return false
}
func (s *cancelStream) Event() provider.Event { return s.cur }
func (s *cancelStream) Err() error            { return s.ctx.Err() }
func (s *cancelStream) Close() error          { return nil }

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
		"первый ход остановлен, потом ok":            {{false, "cancel-empty"}, {false, ok}},
		"ошибка, остановка с текстом, ok":            {{false, "fail"}, {false, "cancel-partial"}, {false, ok}},
		"ok, пустой ответ, ok":                       {{false, ok}, {false, "empty"}, {false, ok}},
		"ok, ok, ещё раз остановлен, ещё раз":        {{false, ok}, {false, ok}, {again, "cancel-empty"}, {again, ok}},
		"ok, ещё раз с ошибкой, новый вопрос":        {{false, ok}, {again, "fail"}, {false, ok}},
		"ещё раз два раза подряд":                    {{false, ok}, {again, ok}, {again, ok}},
		"ошибка, ещё раз с остановкой, ещё раз":      {{false, ok}, {false, "fail"}, {again, "cancel-partial"}, {again, ok}},
		"пустой ответ, ещё раз":                      {{false, ok}, {false, "empty"}, {again, ok}},
		"ok, ещё раз с остановкой с текстом, ok, ok": {{false, ok}, {again, "cancel-partial"}, {false, ok}, {false, ok}},
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
			reasons := map[string]string{"ok": "end", "empty": "end", "fail": "error",
				"cancel-empty": "cancelled", "cancel-partial": "cancelled"}
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
			sess, err := s2.session(s2.conv(convID), config.Profile{}, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := flatMessages(sess.Messages), flatMessages(live); got != want {
				t.Fatalf("после перезапуска:\n  %s\nживая сессия:\n  %s", got, want)
			}
			if !alternates(live) || !alternates(sess.Messages) {
				t.Fatalf("роли не чередуются: живая %s, восстановленная %s", flatMessages(live), flatMessages(sess.Messages))
			}
		})
	}
}
