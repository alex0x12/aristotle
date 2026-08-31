package chat

import (
	"context"
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

type fakeFinder struct {
	hits  []corpus.Hit
	calls int
}

func (f *fakeFinder) Search(context.Context, string, string, int) ([]corpus.Hit, error) {
	f.calls++
	return f.hits, nil
}

func chapterHit(cite, text string) corpus.Hit {
	return corpus.Hit{Chapter: corpus.Chapter{Cite: cite, Text: text}, Score: 0.9}
}

func TestTurnWithoutTools(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "Начнём "},
		{Kind: provider.EventText, Text: "с явлений."},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := &Session{Provider: p, Search: &fakeFinder{}}

	var got strings.Builder
	res, err := s.Turn(context.Background(), "что такое счастье", func(d string) {
		got.WriteString(d)
	})
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
	s := &Session{Provider: p, Search: f}

	res, err := s.Turn(context.Background(), "что такое счастье", func(string) {})
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
		!strings.Contains(res.Chapters[0], "activity of soul") {
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
	s := &Session{Provider: p, Search: f}
	if _, err := s.Turn(context.Background(), "х", func(string) {}); err != nil {
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
	s := &Session{Provider: p, Search: &fakeFinder{}}
	if _, err := s.Turn(context.Background(), "х", func(string) {}); err != nil {
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
	s := &Session{Provider: p, Search: f}

	res, err := s.Turn(context.Background(), "х", func(string) {})
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
