package chat

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alex0x12/aristotle/internal/provider"
)

// pastTurn — прошлый ход: вопрос, вызов поиска, выдержка, ответ.
// Символов в ходе: вопрос 2 + вызов 26 (имя 13 + аргументы {"query":"x"} 13)
// + выдержка + ответ.
func pastTurn(q, excerpt, answer string) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Text: q},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "id-" + q, Name: "search_corpus", Input: map[string]any{"query": "x"}}}},
		{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{ID: "id-" + q, Content: excerpt}}},
		{Role: provider.RoleAssistant, Text: answer},
	}
}

// history — три прошлых хода по 4528 символов и текущий вопрос «в3»:
// 13 586 символов, оценка 4483 токена при доле 0,33.
func history() []provider.Message {
	excerpt := strings.Repeat("а", 3000)
	answer := strings.Repeat("б", 1500)
	var msgs []provider.Message
	for _, q := range []string{"в0", "в1", "в2"} {
		msgs = append(msgs, pastTurn(q, excerpt, answer)...)
	}
	return append(msgs, provider.Message{Role: provider.RoleUser, Text: "в3"})
}

func TestViewWithoutBudgetPassesThrough(t *testing.T) {
	s := &Session{Messages: history()}
	out, trim, fits := s.view(0, 12)
	if len(out) != 13 || trim.Any() || !fits {
		t.Fatalf("len %d, trim %+v, fits %v", len(out), trim, fits)
	}
}

func TestViewOmitsOldestExcerptsFirst(t *testing.T) {
	s := &Session{Messages: history(), Budget: 3600}
	out, trim, fits := s.view(0, 12)
	if trim != (Trim{Excerpts: 1}) || !fits {
		t.Fatalf("trim %+v, fits %v", trim, fits)
	}
	if len(out) != 13 {
		t.Fatalf("ходы не должны выпадать: len %d", len(out))
	}
	if out[2].ToolResults[0].Content != "[выдержки опущены]" {
		t.Fatalf("выдержка первого хода = %q", out[2].ToolResults[0].Content[:20])
	}
	if utf8.RuneCountInString(out[6].ToolResults[0].Content) != 3000 {
		t.Fatal("выдержка второго хода должна остаться")
	}
	if out[1].ToolCalls[0].ID != out[2].ToolResults[0].ID {
		t.Fatal("пара вызов — результат разорвана")
	}
	if utf8.RuneCountInString(s.Messages[2].ToolResults[0].Content) != 3000 {
		t.Fatal("урезание тронуло историю сессии, а не только запрос")
	}
}

func TestViewDropsOldTurnsKeepingFirst(t *testing.T) {
	s := &Session{Messages: history(), Budget: 1500}
	out, trim, fits := s.view(0, 12)
	if trim != (Trim{Excerpts: 3, Turns: 1}) || !fits {
		t.Fatalf("trim %+v, fits %v", trim, fits)
	}
	if len(out) != 9 || out[0].Text != "в0" || out[4].Text != "в2" || out[8].Text != "в3" {
		t.Fatalf("остались не те ходы: %d сообщений", len(out))
	}
}

func TestViewNeverDropsFirstOrCurrent(t *testing.T) {
	s := &Session{Messages: history(), Budget: 10}
	out, trim, fits := s.view(0, 12)
	if fits {
		t.Fatal("бюджет в 10 токенов не может быть соблюдён")
	}
	if trim != (Trim{Excerpts: 3, Turns: 2}) {
		t.Fatalf("trim %+v", trim)
	}
	if len(out) != 5 || out[0].Text != "в0" || out[4].Text != "в3" {
		t.Fatalf("первый и текущий ходы обязаны остаться: %d сообщений", len(out))
	}
}

func TestTurnSendsTrimmedViewButKeepsHistory(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "ответ"},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	past := history()[:12]
	s := &Session{Provider: p, Corpus: &fakeFinder{}, Messages: past, Budget: 1500}
	res, err := s.Turn(context.Background(), "в3", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Trim.Excerpts != 3 || res.Trim.Turns < 1 {
		t.Fatalf("trim %+v", res.Trim)
	}
	sent := p.seen[0].Messages
	if sent[0].Text != "в0" || sent[len(sent)-1].Text != "в3" || len(sent) >= 13 {
		t.Fatalf("в запрос ушло %d сообщений", len(sent))
	}
	if len(s.Messages) != 14 {
		t.Fatalf("история сессии должна быть целой: %d сообщений", len(s.Messages))
	}
}

func TestTurnCalibratesRatioFromUsage(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "ответ"},
		{Kind: provider.EventUsage, Usage: provider.Usage{Input: 400, CacheRead: 600, Output: 5, Known: true}},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}, System: "ты Аристотель"}
	if _, err := s.Turn(context.Background(), "вопрос", nil, nil); err != nil {
		t.Fatal(err)
	}
	chars := utf8.RuneCountInString(s.System) +
		toolsChars([]provider.ToolDef{provider.SearchCorpusTool()}) +
		messageChars(provider.Message{Role: provider.RoleUser, Text: "вопрос"})
	want := 1000.0 / float64(chars)
	if s.tokenRatio != want || s.LastPrompt != 1000 {
		t.Fatalf("доля %v (ожидалось %v), LastPrompt %d", s.tokenRatio, want, s.LastPrompt)
	}
}

func TestTurnWithoutUsageEstimatesLastPrompt(t *testing.T) {
	p := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventText, Text: "ответ"},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := &Session{Provider: p, Corpus: &fakeFinder{}}
	if _, err := s.Turn(context.Background(), "вопрос", nil, nil); err != nil {
		t.Fatal(err)
	}
	if s.tokenRatio != 0 || s.LastPrompt <= 0 {
		t.Fatalf("доля %v, LastPrompt %d", s.tokenRatio, s.LastPrompt)
	}
}
