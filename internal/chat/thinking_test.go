package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
)

// sse собирает поток DeepSeek из кусков дельт; последний кусок несёт finish_reason и расход.
func sse(finish string, deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString(`data: {"choices":[{"index":0,"delta":` + d + `}]}` + "\n\n")
	}
	b.WriteString(`data: {"choices":[{"index":0,"delta":{},"finish_reason":"` + finish + `"}],` +
		`"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":100}}` + "\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// Рассуждение возвращается серверу и внутри хода (вместе с вызовом поиска),
// и в следующем ходу: сервер в режиме размышления требует этого в каждом
// запросе с инструментами. Настоящий Session и настоящий провайдер против
// сценарного сервера.
func TestReasoningGoesBackInsideToolLoopAndAcrossTurns(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	script := []string{
		// запрос 1: рассуждение кусками, потом вызов поиска
		sse("tool_calls",
			`{"reasoning_content":"р1а"}`, `{"reasoning_content":"р1б"}`,
			`{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search_corpus","arguments":"{\"query\":\"virtue\"}"}}]}`),
		// запрос 2 (тот же ход, после поиска): рассуждение и ответ
		sse("stop", `{"reasoning_content":"р2"}`, `{"content":"Ответ."}`),
		// запрос 3 (следующий ход): без вызова поиска
		sse("stop", `{"reasoning_content":"р3"}`, `{"content":"Ещё."}`),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		n := len(bodies)
		bodies = append(bodies, b)
		mu.Unlock()
		io.WriteString(w, script[n])
	}))
	defer srv.Close()

	s := &Session{Provider: openaicompat.New(srv.URL, "m", "к", "high"), Corpus: &fakeFinder{}}
	res, err := s.Turn(context.Background(), "что такое добродетель?", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Ответ." || res.Reasoning != "р1ар1б\n\nр2" {
		t.Fatalf("ход 1: текст %q, рассуждение %q", res.Text, res.Reasoning)
	}
	if _, err := s.Turn(context.Background(), "а дружба?", nil, nil); err != nil {
		t.Fatal(err)
	}

	assistants := func(body map[string]any) (out []map[string]any) {
		for _, m := range body["messages"].([]any) {
			if mm := m.(map[string]any); mm["role"] == "assistant" {
				out = append(out, mm)
			}
		}
		return
	}
	// Запрос 1: истории ответов нет, параметры режима на месте.
	if len(assistants(bodies[0])) != 0 || bodies[0]["reasoning_effort"] != "high" {
		t.Fatalf("запрос 1: %+v", bodies[0])
	}
	// Запрос 2: ответ с вызовом поиска несёт своё рассуждение.
	a2 := assistants(bodies[1])
	if len(a2) != 1 || a2[0]["reasoning_content"] != "р1ар1б" || a2[0]["tool_calls"] == nil {
		t.Fatalf("запрос 2: рассуждение внутри хода не вернулось: %+v", a2)
	}
	// Запрос 3 (следующий ход): все прежние ответы — с рассуждениями.
	a3 := assistants(bodies[2])
	if len(a3) != 2 || a3[0]["reasoning_content"] != "р1ар1б" || a3[1]["reasoning_content"] != "р2" {
		t.Fatalf("запрос 3: рассуждения прежнего хода потеряны: %+v", a3)
	}
	if a3[1]["content"] != "Ответ." {
		t.Errorf("итоговый ответ прежнего хода: %+v", a3[1])
	}
}

// Рассуждение — часть контекста: оценка размера его учитывает, а целый старый
// ход уходит вместе со своим рассуждением.
func TestBudgetCountsReasoning(t *testing.T) {
	plain := provider.Message{Role: provider.RoleAssistant, Text: "ответ"}
	withR := provider.Message{Role: provider.RoleAssistant, Text: "ответ", Reasoning: strings.Repeat("р", 100)}
	if messageChars(withR)-messageChars(plain) != 100 {
		t.Fatalf("рассуждение не посчитано: %d и %d", messageChars(plain), messageChars(withR))
	}
	big := strings.Repeat("р", 20000)
	s := &Session{Budget: 1000, Messages: []provider.Message{
		{Role: provider.RoleUser, Text: "в0"}, {Role: provider.RoleAssistant, Text: "о0"},
		{Role: provider.RoleUser, Text: "в1"}, {Role: provider.RoleAssistant, Text: "о1", Reasoning: big},
		{Role: provider.RoleUser, Text: "в2"},
	}}
	out, trim, fits := s.view(0, 4)
	if trim.Turns != 1 || !fits || len(out) != 3 {
		t.Fatalf("ход с длинным рассуждением должен уйти целиком: trim %+v, fits %v, сообщений %d", trim, fits, len(out))
	}
	for _, m := range out {
		if m.Reasoning != "" {
			t.Errorf("рассуждение осталось в урезанном виде: %+v", m)
		}
	}
}
