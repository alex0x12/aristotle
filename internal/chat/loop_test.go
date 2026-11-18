package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// scriptedProvider отдаёт заранее записанные потоки, по одному на запрос.
type scriptedProvider struct {
	turns [][]provider.Event
	seen  []provider.Request
}

func (p *scriptedProvider) Stream(_ context.Context, req provider.Request) (provider.Stream, error) {
	p.seen = append(p.seen, req)
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

// erroringProvider падает уже на Stream — сетевой сбой, лимит запросов и т.п.
type erroringProvider struct {
	err error
}

func (p *erroringProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, p.err
}

// streamErrorProvider отдаёт поток, который падает не сразу, а через Err()
// после исчерпания событий — второй путь отказа посреди хода.
type streamErrorProvider struct {
	err error
}

func (p *streamErrorProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return &erroringStream{err: p.err}, nil
}

type erroringStream struct{ err error }

func (s *erroringStream) Next() bool            { return false }
func (s *erroringStream) Event() provider.Event { return provider.Event{} }
func (s *erroringStream) Err() error            { return s.err }
func (s *erroringStream) Close() error          { return nil }

type fakeFinder struct {
	hits  []corpus.Hit
	calls int
}

func (f *fakeFinder) Search(context.Context, string, string, int) ([]corpus.Hit, error) {
	f.calls++
	return f.hits, nil
}

// capturingFinder запоминает, каким пришёл k — нужен, чтобы проверить, что
// float64 из JSON-аргументов инструмента доезжает до поиска целым числом.
type capturingFinder struct {
	hits []corpus.Hit
	gotK int
}

func (f *capturingFinder) Search(_ context.Context, _, _ string, k int) ([]corpus.Hit, error) {
	f.gotK = k
	return f.hits, nil
}

func chapterHit(cite, text string) corpus.Hit {
	return corpus.Hit{Chapter: corpus.Chapter{Cite: cite, Text: text}, Score: 0.9}
}

// erroringFinder имитирует отказ поиска: qmd нет в PATH, коллекция не
// проиндексирована и т.п.
type erroringFinder struct{ err error }

func (f *erroringFinder) Search(context.Context, string, string, int) ([]corpus.Hit, error) {
	return nil, f.err
}

func TestTurnWithoutTools(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "Начнём "},
		{Kind: provider.EventText, Text: "с явлений."},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}

	var got strings.Builder
	res, err := s.Turn(context.Background(), "что такое счастье", func(d string) {
		got.WriteString(d)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Начнём с явлений." {
		t.Errorf("поток = %q", got.String())
	}
	if res.Iterations != 0 {
		t.Errorf("Iterations = %d, инструмент не звали", res.Iterations)
	}
}

func TestTurnRunsToolThenAnswers(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{
			{Kind: provider.EventText, Text: "Благо человека есть деятельность души."},
			{Kind: provider.EventDone, StopReason: "end_turn"},
		},
	}}
	f := &fakeFinder{hits: []corpus.Hit{
		chapterHit("Nicomachean Ethics I.7", "the good of man is an activity of soul")}}
	s := &Session{Provider: p, Corpus: f}

	res, err := s.Turn(context.Background(), "что такое счастье", func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Errorf("поиск вызван %d раз", f.calls)
	}
	if res.Iterations != 1 {
		t.Errorf("Iterations = %d", res.Iterations)
	}
	if len(res.Chapters) != 1 ||
		res.Chapters[0].Cite != "Nicomachean Ethics I.7" ||
		!strings.Contains(res.Chapters[0].Text, "activity of soul") {
		t.Errorf("Chapters = %+v", res.Chapters)
	}
	if res.Exhausted {
		t.Error("потолок не достигнут, Exhausted должен быть false")
	}
}

func TestToolResultCarriesCite(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &fakeFinder{hits: []corpus.Hit{
		chapterHit("Nicomachean Ethics I.7", "тело главы")}}
	s := &Session{Provider: p, Corpus: f}
	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	// Второй запрос должен нести результат инструмента с ссылкой.
	last := p.seen[len(p.seen)-1]
	var found bool
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			if strings.Contains(r.Content, "Nicomachean Ethics I.7") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("в результате инструмента нет ссылки на главу")
	}
}

func TestEmptySearchIsReportedNotError(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "nginx"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	last := p.seen[len(p.seen)-1]
	var content string
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content = r.Content
		}
	}
	if !strings.Contains(content, "ничего не найдено") {
		t.Fatalf("пустая выдача должна сообщаться словами, получено %q", content)
	}
}

func TestCeilingStopsLoopAndMarksExhausted(t *testing.T) {
	toolTurn := []provider.Event{
		{Kind: provider.EventToolCall, Call: provider.ToolCall{
			ID: "c", Name: "search_corpus",
			Input: map[string]any{"query": "х"}}},
		{Kind: provider.EventDone, StopReason: "tool_use"},
	}
	turns := [][]provider.Event{toolTurn, toolTurn, toolTurn, toolTurn,
		{{Kind: provider.EventText, Text: "отвечаю тем, что нашёл"},
			{Kind: provider.EventDone, StopReason: "end_turn"}}}
	p := &scriptedProvider{turns: turns}
	f := &fakeFinder{hits: []corpus.Hit{chapterHit("X I.1", "тело")}}
	s := &Session{Provider: p, Corpus: f}

	res, err := s.Turn(context.Background(), "х", func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Iterations != MaxToolIterations {
		t.Errorf("Iterations = %d, ожидалось %d", res.Iterations, MaxToolIterations)
	}
	if !res.Exhausted {
		t.Error("на потолке Exhausted должен быть true")
	}
	last := p.seen[len(p.seen)-1]
	var content string
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content = r.Content
		}
	}
	if !strings.Contains(content, "поиск исчерпан") {
		t.Fatalf("на потолке модель должна получить пометку, получено %q", content)
	}
	if res.Text != "отвечаю тем, что нашёл" {
		t.Errorf("Text = %q", res.Text)
	}
}

// TestCeilingWithEmptyLastSearchStillMarksExhausted закрывает дефект №1:
// renderHits раньше возвращался на пустой выдаче до того, как дописать
// пометку об исчерпании. На потолке пустой поиск как раз вероятен — модель
// дошла до предела именно потому, что предыдущие попытки её не устроили.
func TestCeilingWithEmptyLastSearchStillMarksExhausted(t *testing.T) {
	toolTurn := []provider.Event{
		{Kind: provider.EventToolCall, Call: provider.ToolCall{
			ID: "c", Name: "search_corpus",
			Input: map[string]any{"query": "х"}}},
		{Kind: provider.EventDone, StopReason: "tool_use"},
	}
	turns := [][]provider.Event{toolTurn, toolTurn, toolTurn, toolTurn,
		{{Kind: provider.EventText, Text: "не нашёл"},
			{Kind: provider.EventDone, StopReason: "end_turn"}}}
	p := &scriptedProvider{turns: turns}
	f := &fakeFinder{} // без хитов на всех четырёх обращениях
	s := &Session{Provider: p, Corpus: f}

	res, err := s.Turn(context.Background(), "х", func(string) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Exhausted {
		t.Error("на потолке Exhausted должен быть true")
	}
	last := p.seen[len(p.seen)-1]
	var content string
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content = r.Content
		}
	}
	if !strings.Contains(content, "ничего не найдено") {
		t.Errorf("пустая выдача на потолке всё равно должна сообщаться словами, получено %q", content)
	}
	if !strings.Contains(content, "поиск исчерпан") {
		t.Errorf("пометка об исчерпании не должна теряться при пустой выдаче, получено %q", content)
	}
}

// TestCeilingOmitsToolFromNextRequest закрывает дефект №2: потолок должен
// быть обеспечен устройством, а не только текстовой просьбой. Как только
// достигнут, инструмент физически не предлагается в следующем запросе.
func TestCeilingOmitsToolFromNextRequest(t *testing.T) {
	toolTurn := []provider.Event{
		{Kind: provider.EventToolCall, Call: provider.ToolCall{
			ID: "c", Name: "search_corpus",
			Input: map[string]any{"query": "х"}}},
		{Kind: provider.EventDone, StopReason: "tool_use"},
	}
	turns := [][]provider.Event{toolTurn, toolTurn, toolTurn, toolTurn,
		{{Kind: provider.EventText, Text: "отвечаю"},
			{Kind: provider.EventDone, StopReason: "end_turn"}}}
	p := &scriptedProvider{turns: turns}
	f := &fakeFinder{hits: []corpus.Hit{chapterHit("X I.1", "тело")}}
	s := &Session{Provider: p, Corpus: f}

	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.seen) != 5 {
		t.Fatalf("ожидалось 5 запросов к провайдеру, получено %d", len(p.seen))
	}
	last := p.seen[len(p.seen)-1]
	if len(last.Tools) != 0 {
		t.Errorf("на потолке инструмент не должен предлагаться, Tools = %+v", last.Tools)
	}
}

// TestFailedTurnLeavesHistoryUnchanged закрывает дефект №3: реплика
// пользователя добавляется в историю до обращения к провайдеру, и если
// обращение падает, история не должна остаться с репликой без ответа —
// иначе следующий ход допишет ещё одну пользовательскую реплику подряд.
func TestFailedTurnLeavesHistoryUnchanged(t *testing.T) {
	p := &erroringProvider{err: errors.New("сеть недоступна")}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	before := len(s.Messages)

	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if len(s.Messages) != before {
		t.Errorf("после неудачного хода длина истории = %d, ожидалось %d",
			len(s.Messages), before)
	}
}

// TestFailedStreamLeavesHistoryUnchanged — второй путь отказа: поток
// открылся, но упал при чтении (Err() после исчерпания событий). Тот же
// откат обязателен и здесь.
func TestFailedStreamLeavesHistoryUnchanged(t *testing.T) {
	p := &streamErrorProvider{err: errors.New("поток оборвался")}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	before := len(s.Messages)

	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if len(s.Messages) != before {
		t.Errorf("после неудачного хода длина истории = %d, ожидалось %d",
			len(s.Messages), before)
	}
}

// TestArgIntConvertsFloat64FromJSON закрывает дефект №4: encoding/json при
// разборе в map[string]any отдаёт числа как float64, а не int. Проверяем
// сквозь весь ход, что k доезжает до поиска целым числом.
func TestArgIntConvertsFloat64FromJSON(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness", "k": float64(3)}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &capturingFinder{}
	s := &Session{Provider: p, Corpus: f}
	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	if f.gotK != 3 {
		t.Errorf("k дошёл как %d, ожидалось 3 (аргумент был float64)", f.gotK)
	}
}

// TestSearchFailureCallsOnSearchWarning закрывает дефект итогового ревью
// ветки: сломанный поиск был неотличим от честно молчащего корпуса. Ошибка
// поиска и раньше доходила до модели текстом "поиск не удался", но никогда
// не доходила до пользователя — на экране это выглядело как обычный
// уверенный ответ без ссылок, то есть точно так же, как правильный ответ на
// честно пустой корпус (5.5). Колбэк обязан сработать с сообщением,
// содержащим причину отказа.
func TestSearchFailureCallsOnSearchWarning(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventText, Text: "отвечаю без выдержек"},
			{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	cause := errors.New("exec: \"qmd\": executable file not found in $PATH")
	f := &erroringFinder{err: cause}
	var warnings []string
	s := &Session{Provider: p, Corpus: f, OnSearchWarning: func(msg string) {
		warnings = append(warnings, msg)
	}}

	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Fatalf("OnSearchWarning вызван %d раз, ожидался 1: %+v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], cause.Error()) {
		t.Errorf("предупреждение не содержит причину отказа: %q", warnings[0])
	}
}

// TestSearchFailureWithoutCallbackStillReportsToModel проверяет, что
// отсутствие OnSearchWarning (например, при вызове из eval) не роняет ход:
// колбэк необязателен, модель всё равно получает текст об отказе.
func TestSearchFailureWithoutCallbackStillReportsToModel(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &erroringFinder{err: errors.New("коллекция не проиндексирована")}
	s := &Session{Provider: p, Corpus: f}

	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	last := p.seen[len(p.seen)-1]
	var content string
	var isError bool
	for _, m := range last.Messages {
		for _, r := range m.ToolResults {
			content, isError = r.Content, r.IsError
		}
	}
	if !isError || !strings.Contains(content, "поиск не удался") {
		t.Errorf("модель должна получить текст отказа, получено %q (IsError=%v)", content, isError)
	}
}

// TestOnChaptersFiresBeforeTurnReturns закрывает главный дефект финального
// ревью: печать не должна ждать конца хода. Главы известны раньше, чем
// модель напишет первую выдержку — сразу после обращения к поиску, — и
// onChapters обязан сообщить о них в этот момент, а не когда Turn уже вернул
// управление.
func TestOnChaptersFiresBeforeTurnReturns(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{
			{Kind: provider.EventText, Text: "Благо человека — деятельность души."},
			{Kind: provider.EventDone, StopReason: "end_turn"},
		},
	}}
	f := &fakeFinder{hits: []corpus.Hit{
		chapterHit("Nicomachean Ethics I.7", "the good of man is an activity of soul")}}
	s := &Session{Provider: p, Corpus: f}

	var sawChaptersBeforeText bool
	var gotChapters []corpus.Chapter
	res, err := s.Turn(context.Background(), "что такое счастье",
		func(string) {
			// К моменту первой текстовой дельты главы обязаны уже быть
			// известны колбэку — это и есть суть починки.
			if len(gotChapters) > 0 {
				sawChaptersBeforeText = true
			}
		},
		func(cs []corpus.Chapter) {
			gotChapters = cs
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotChapters) != 1 || gotChapters[0].Cite != "Nicomachean Ethics I.7" {
		t.Fatalf("onChapters получил %+v", gotChapters)
	}
	if !sawChaptersBeforeText {
		t.Error("главы должны быть переданы через onChapters ДО первой текстовой дельты")
	}
	if len(res.Chapters) != 1 {
		t.Errorf("итоговый TurnResult.Chapters = %+v", res.Chapters)
	}
}

// TestOnChaptersOptional проверяет, что nil в качестве onChapters не роняет
// ход — вызывающая сторона (например eval) вправе его не передавать.
func TestOnChaptersOptional(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "happiness"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &fakeFinder{hits: []corpus.Hit{chapterHit("X I.1", "тело")}}
	s := &Session{Provider: p, Corpus: f}
	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
}

// TestOnSearchStartFiresBeforeEachSearch проверяет, что колбэк о начале
// поиска срабатывает перед обращением к Corpus.Search — это то, что main.go
// печатает в stderr, чтобы пользователь видел, что программа не зависла.
func TestOnSearchStartFiresBeforeEachSearch(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "c1", Name: "search_corpus",
				Input: map[string]any{"query": "leisure", "group": "ethics"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &fakeFinder{hits: []corpus.Hit{chapterHit("X I.1", "тело")}}
	var starts []string
	s := &Session{Provider: p, Corpus: f, OnSearchStart: func(query, group string) {
		starts = append(starts, query+"|"+group)
		if f.calls != 0 {
			t.Error("OnSearchStart должен сработать ДО обращения к поиску")
		}
	}}
	if _, err := s.Turn(context.Background(), "х", func(string) {}, nil); err != nil {
		t.Fatal(err)
	}
	if len(starts) != 1 || starts[0] != "leisure|ethics" {
		t.Errorf("starts = %+v", starts)
	}
}

func TestTurnSumsUsageAcrossRequests(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{ID: "t1", Name: "search_corpus", Input: map[string]any{"query": "virtue"}}},
			{Kind: provider.EventUsage, Usage: provider.Usage{Input: 100, Output: 10, CacheRead: 1000, Known: true}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{
			{Kind: provider.EventText, Text: "ответ"},
			{Kind: provider.EventUsage, Usage: provider.Usage{Input: 50, Output: 200, CacheRead: 1100, CacheWrite: 7, Known: true}},
			{Kind: provider.EventDone, StopReason: "end_turn"},
		},
	}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	res, err := s.Turn(context.Background(), "что такое добродетель?", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := provider.Usage{Input: 150, Output: 210, CacheRead: 2100, CacheWrite: 7, Known: true}
	if res.Usage != want {
		t.Fatalf("расход хода = %+v, ожидалось %+v", res.Usage, want)
	}
}

func TestTurnUsageUnknownWhenARequestIsSilent(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{ID: "t1", Name: "search_corpus", Input: map[string]any{"query": "virtue"}}},
			{Kind: provider.EventUsage, Usage: provider.Usage{Input: 100, Output: 10, Known: true}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{
			{Kind: provider.EventText, Text: "ответ"},
			{Kind: provider.EventDone, StopReason: "end_turn"},
		},
	}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	res, err := s.Turn(context.Background(), "вопрос", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Known {
		t.Fatalf("один запрос без расхода делает сумму неизвестной: %+v", res.Usage)
	}
}

// failingSecondProvider проводит первый запрос по сценарию, а второй роняет:
// либо сразу на Stream, либо через Err() потока. Нужен, чтобы проверить итог
// хода, оборвавшегося после того, как расход первого запроса уже учтён.
type failingSecondProvider struct {
	first    []provider.Event
	err      error
	viaErr   bool // false — падает Stream, true — падает Err() потока
	requests int
}

func (p *failingSecondProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	p.requests++
	if p.requests == 1 {
		return &scriptedStream{events: p.first}, nil
	}
	if p.viaErr {
		return &erroringStream{err: p.err}, nil
	}
	return nil, p.err
}

// TestFailedTurnMarksUsageUnknown: когда второй запрос хода падает, сумма
// первого остаётся в Usage, но расход оборванного запроса неизвестен — сумма
// занижена, и без Known = false цифра на экране выглядела бы полной.
func TestFailedTurnMarksUsageUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		viaErr bool
	}{
		{"Stream падает сразу", false},
		{"Err() потока после чтения", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New("поток оборвался")
			p := &failingSecondProvider{err: cause, viaErr: tc.viaErr, first: []provider.Event{
				{Kind: provider.EventToolCall, Call: provider.ToolCall{
					ID: "c1", Name: "search_corpus",
					Input: map[string]any{"query": "happiness"}}},
				{Kind: provider.EventUsage, Usage: provider.Usage{
					Input: 100, Output: 10, CacheRead: 1000, Known: true}},
				{Kind: provider.EventDone, StopReason: "tool_use"},
			}}
			s := &Session{Provider: p, Corpus: &fakeFinder{}}

			res, err := s.Turn(context.Background(), "х", func(string) {}, nil)
			if !errors.Is(err, cause) {
				t.Fatalf("ожидалась ошибка провайдера, получено %v", err)
			}
			if res.Usage.Input != 100 || res.Usage.Output != 10 || res.Usage.CacheRead != 1000 {
				t.Errorf("сумма первого запроса потеряна: %+v", res.Usage)
			}
			if res.Usage.Known {
				t.Error("Known должен быть false: расход упавшего запроса неизвестен")
			}
		})
	}
}

func TestDefaultKUsedWhenModelGivesNone(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{ID: "t", Name: "search_corpus", Input: map[string]any{"query": "x"}}},
			{Kind: provider.EventDone, StopReason: "tool_use"},
		},
		{{Kind: provider.EventText, Text: "ответ"}, {Kind: provider.EventDone, StopReason: "end_turn"}},
	}}
	f := &capturingFinder{}
	s := &Session{Provider: p, Corpus: f, DefaultK: 3}
	if _, err := s.Turn(context.Background(), "вопрос", nil, nil); err != nil {
		t.Fatal(err)
	}
	if f.gotK != 3 {
		t.Fatalf("k = %d, ожидалось 3", f.gotK)
	}
}
