package anthropic

import (
	"testing"

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
