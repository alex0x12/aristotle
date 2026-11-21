package opencodecli

import (
	"context"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
)

func collect(t *testing.T, s provider.Stream) []provider.Event {
	t.Helper()
	var out []provider.Event
	for s.Next() {
		out = append(out, s.Event())
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	return out
}

func TestNeedsSearchOnlyUntilToolResult(t *testing.T) {
	first := []provider.Message{{Role: provider.RoleUser, Text: "что такое дружба?"}}
	if !needsSearch(first) {
		t.Fatal("первый запрос хода должен просить поиск")
	}
	withResult := append(first,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "search_corpus"}}},
		provider.Message{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{ID: "1", Content: "выдержки"}}})
	if needsSearch(withResult) {
		t.Fatal("после результата инструмента поиск не нужен")
	}
	nextTurn := append(withResult, provider.Message{Role: provider.RoleUser, Text: "а справедливость?"})
	if !needsSearch(nextTurn) {
		t.Fatal("новый вопрос — снова поиск")
	}
}

func TestFirstStreamReturnsSearchToolCall(t *testing.T) {
	c := New("deepseek-v4.1-flash-free", "high").(*client)
	s, err := c.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "что такое добродетель?"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, s)
	if len(evs) != 3 || evs[0].Kind != provider.EventToolCall {
		t.Fatalf("события: %+v", evs)
	}
	if evs[0].Call.Name != "search_corpus" || evs[0].Call.Input["query"] != "что такое добродетель?" {
		t.Fatalf("вызов: %+v", evs[0].Call)
	}
}

func TestBuildPromptHasSystemAndExcerpts(t *testing.T) {
	msg := provider.Message{Role: provider.RoleUser}
	msg.ToolResults = []provider.ToolResult{{ID: "1", Content: "### Никомахова этика I.1\nтекст"}}
	got := buildPrompt(provider.Request{
		System: "ПЕРСОНА",
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: "вопрос"},
			msg,
		},
	})
	for _, want := range []string{"ПЕРСОНА", "ВЫДЕРЖКИ ИЗ КОРПУСА", "Никомахова этика I.1", "СОБЕСЕДНИК: вопрос"} {
		if !strings.Contains(got, want) {
			t.Errorf("в запросе нет %q:\n%s", want, got)
		}
	}
}

func TestModelArgDoesNotDoublePrefix(t *testing.T) {
	if modelArg("longcat-2.5-free") != "opencode/longcat-2.5-free" {
		t.Fatal("префикс opencode/ не добавлен")
	}
	if modelArg("opencode/beam-free") != "opencode/beam-free" {
		t.Fatal("двойной префикс")
	}
}
