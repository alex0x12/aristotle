package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/alex0x12/aristotle/internal/provider"
)

func TestRequiredFieldsAcceptsStringSlice(t *testing.T) {
	// Форма, которую кладёт наш собственный SearchCorpusTool().
	schema := map[string]any{"required": []string{"query"}}
	got, ok := requiredFields(schema)
	if !ok {
		t.Fatal("ожидался успех для []string")
	}
	if len(got) != 1 || got[0] != "query" {
		t.Fatalf("required = %+v, ожидалось [query]", got)
	}
}

func TestRequiredFieldsAcceptsAnySliceOfStrings(t *testing.T) {
	// Форма, в которую encoding/json разворачивает JSON-массив строк.
	schema := map[string]any{"required": []any{"query", "group"}}
	got, ok := requiredFields(schema)
	if !ok {
		t.Fatal("ожидался успех для []any из строк")
	}
	if len(got) != 2 || got[0] != "query" || got[1] != "group" {
		t.Fatalf("required = %+v, ожидалось [query group]", got)
	}
}

func TestRequiredFieldsAbsentIsNotAnError(t *testing.T) {
	schema := map[string]any{"properties": map[string]any{}}
	got, ok := requiredFields(schema)
	if !ok {
		t.Fatal("отсутствие required — не ошибка")
	}
	if len(got) != 0 {
		t.Fatalf("required = %+v, ожидался пустой список", got)
	}
}

func TestRequiredFieldsRejectsForeignType(t *testing.T) {
	// Посторонний тип не должен молча превращаться в пустой список.
	schema := map[string]any{"required": 42}
	if _, ok := requiredFields(schema); ok {
		t.Fatal("посторонний тип required должен вернуть ok=false")
	}
}

func TestRequiredFieldsRejectsAnySliceWithNonString(t *testing.T) {
	schema := map[string]any{"required": []any{"query", 5}}
	if _, ok := requiredFields(schema); ok {
		t.Fatal("[]any с не-строковым элементом должен вернуть ok=false")
	}
}

func TestParamsUsesProfileMaxTokensOverride(t *testing.T) {
	c := &client{model: "claude-opus-5", maxTokens: 4096}
	p, err := c.params(provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d, ожидалось 4096 (потолок профиля)", p.MaxTokens)
	}
}

func TestParamsWithoutProfileOverrideUsesDefault(t *testing.T) {
	c := &client{model: "claude-opus-5"}
	p, err := c.params(provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxTokens != provider.DefaultMaxTokens {
		t.Fatalf("MaxTokens = %d, ожидалось %d (умолчание)", p.MaxTokens, provider.DefaultMaxTokens)
	}
}

func TestParamsRejectsUnparsableRequired(t *testing.T) {
	c := &client{model: "claude-opus-5"}
	req := provider.Request{
		Tools: []provider.ToolDef{{
			Name:        "broken_tool",
			Description: "инструмент с непригодным required",
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   42, // ни []string, ни []any из строк
			},
		}},
	}
	if _, err := c.params(req); err == nil {
		t.Fatal("params должен вернуть ошибку при непригодном required")
	}
}

// anthropicStream — запись настоящего потока Messages API, сокращённая.
// message_start несёт вход и кэш, message_delta — итоговый вывод.
const anthropicStream = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":120,"cache_creation_input_tokens":300,"cache_read_input_tokens":18000,"output_tokens":1}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Добродетель"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":42}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

func TestStreamReportsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, anthropicStream)
	}))
	defer srv.Close()

	c := &client{
		api:   sdk.NewClient(option.WithAPIKey("тест"), option.WithBaseURL(srv.URL)),
		model: "claude-opus-5",
	}
	st, err := c.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var text string
	var got provider.Usage
	for st.Next() {
		ev := st.Event()
		switch ev.Kind {
		case provider.EventText:
			text += ev.Text
		case provider.EventUsage:
			got = ev.Usage
		}
	}
	if err := st.Err(); err != nil {
		t.Fatal(err)
	}
	want := provider.Usage{Input: 120, Output: 42, CacheRead: 18000, CacheWrite: 300, Known: true}
	if got != want {
		t.Fatalf("расход = %+v, ожидалось %+v", got, want)
	}
	if text != "Добродетель" {
		t.Fatalf("текст = %q", text)
	}
}
