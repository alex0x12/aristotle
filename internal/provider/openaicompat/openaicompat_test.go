package openaicompat

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
)

func TestAccumulatorJoinsByIndex(t *testing.T) {
	var a accumulator
	// Провайдер вправе чередовать индексы: собирать по порядку прибытия нельзя.
	a.Add(0, "call_a", "search_corpus", `{"que`)
	a.Add(1, "call_b", "search_corpus", `{"query":"c`)
	a.Add(0, "", "", `ry":"happiness"}`)
	a.Add(1, "", "", `auses"}`)

	calls := a.Calls()
	if len(calls) != 2 {
		t.Fatalf("получено %d вызовов", len(calls))
	}
	if calls[0].ID != "call_a" || calls[0].Input["query"] != "happiness" {
		t.Errorf("вызов 0: %+v", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].Input["query"] != "causes" {
		t.Errorf("вызов 1: %+v", calls[1])
	}
}

func TestAccumulatorEmptyIsNil(t *testing.T) {
	var a accumulator
	if got := a.Calls(); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAccumulatorSkipsBrokenJSON(t *testing.T) {
	var a accumulator
	a.Add(0, "call_a", "search_corpus", `{"query": не json`)
	if got := a.Calls(); len(got) != 0 {
		t.Fatalf("битый JSON не должен превращаться в вызов: %+v", got)
	}
}

func TestReasoningProfileRenamesMaxTokens(t *testing.T) {
	body := buildBody("deepseek-reasoner", true, 0, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if _, ok := body["max_tokens"]; ok {
		t.Error("у рассуждающей модели max_tokens не принимается")
	}
	if _, ok := body["max_completion_tokens"]; !ok {
		t.Error("нет max_completion_tokens")
	}
	if _, ok := body["temperature"]; ok {
		t.Error("temperature у рассуждающей модели не принимается")
	}
}

func TestPlainProfileUsesMaxTokens(t *testing.T) {
	body := buildBody("deepseek-chat", false, 0, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if _, ok := body["max_tokens"]; !ok {
		t.Error("нет max_tokens")
	}
}

func TestSystemGoesFirstAsMessage(t *testing.T) {
	body := buildBody("gpt-5.1", false, 0, provider.Request{
		System:   "ты Аристотель",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "привет"}},
	})
	msgs := body["messages"].([]map[string]any)
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "ты Аристотель" {
		t.Fatalf("первое сообщение: %+v", msgs[0])
	}
}

// TestProfileMaxTokensOverridesDefault закрывает дефект №7 итогового ревью:
// DefaultMaxTokens=64000 — факт про Anthropic, не про всех провайдеров.
// Профиль с меньшим потолком вывода (например, openrouter, направленный на
// не-Anthropic модель) обязан передать именно свой потолок, а не умолчание,
// иначе каждый запрос отвечает четырёхсотой ошибкой.
func TestProfileMaxTokensOverridesDefault(t *testing.T) {
	body := buildBody("some-model", false, 4096, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if got := body["max_tokens"]; got != 4096 {
		t.Fatalf("max_tokens = %v, ожидалось 4096 (потолок профиля)", got)
	}
}

func TestProfileMaxTokensOverridesDefaultForReasoning(t *testing.T) {
	body := buildBody("some-reasoning-model", true, 4096, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if got := body["max_completion_tokens"]; got != 4096 {
		t.Fatalf("max_completion_tokens = %v, ожидалось 4096 (потолок профиля)", got)
	}
}

func TestRequestMaxTokensOverridesProfile(t *testing.T) {
	body := buildBody("some-model", false, 4096, provider.Request{
		MaxTokens: 500,
		Messages:  []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if got := body["max_tokens"]; got != 500 {
		t.Fatalf("max_tokens = %v, ожидалось 500 (явный per-запросный побеждает потолок профиля)", got)
	}
}

// drain прогоняет записанный поток через разбор и возвращает текст и расход.
func drain(t *testing.T, sse string) (string, provider.Usage) {
	t.Helper()
	s := &stream{body: io.NopCloser(strings.NewReader("")), sc: bufio.NewScanner(strings.NewReader(sse))}
	var text strings.Builder
	var usage provider.Usage
	for s.Next() {
		ev := s.Event()
		switch ev.Kind {
		case provider.EventText:
			text.WriteString(ev.Text)
		case provider.EventUsage:
			usage = ev.Usage
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return text.String(), usage
}

// DeepSeek: расход в последнем куске вместе с finish_reason, кэш своим полем.
const deepseekStream = `data: {"choices":[{"index":0,"delta":{"content":"Да."},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":20000,"completion_tokens":350,"prompt_cache_hit_tokens":18000,"prompt_cache_miss_tokens":2000}}` + "\n\n" +
	"data: [DONE]\n\n"

// OpenAI и OpenRouter: отдельный кусок с пустым choices, кэш в prompt_tokens_details.
const openaiStream = `data: {"choices":[{"index":0,"delta":{"content":"Да."},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
	`data: {"choices":[],"usage":{"prompt_tokens":20000,"completion_tokens":350,"prompt_tokens_details":{"cached_tokens":18000}}}` + "\n\n" +
	"data: [DONE]\n\n"

const silentStream = `data: {"choices":[{"index":0,"delta":{"content":"Да."},"finish_reason":"stop"}]}` + "\n\n" +
	"data: [DONE]\n\n"

func TestUsageDeepSeek(t *testing.T) {
	text, u := drain(t, deepseekStream)
	want := provider.Usage{Input: 2000, Output: 350, CacheRead: 18000, Known: true}
	if text != "Да." || u != want {
		t.Fatalf("текст %q, расход %+v, ожидалось %+v", text, u, want)
	}
}

func TestUsageOpenAIEmptyChoices(t *testing.T) {
	_, u := drain(t, openaiStream)
	want := provider.Usage{Input: 2000, Output: 350, CacheRead: 18000, Known: true}
	if u != want {
		t.Fatalf("расход %+v, ожидалось %+v", u, want)
	}
}

func TestUsageMissingIsUnknown(t *testing.T) {
	_, u := drain(t, silentStream)
	if u.Known {
		t.Fatalf("расход без usage в потоке не может быть известным: %+v", u)
	}
}

func TestBodyAsksForUsage(t *testing.T) {
	body := buildBody("deepseek-v4-pro", false, 0, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	opts, ok := body["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options = %#v", body["stream_options"])
	}
}

func TestTuneEffortOnlyForReasoning(t *testing.T) {
	body := map[string]any{}
	temp := 0.5
	tune(body, true, "high", &temp)
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v", body["reasoning_effort"])
	}
	if _, ok := body["temperature"]; ok {
		t.Fatal("рассуждающая модель temperature не принимает")
	}
	plain := map[string]any{}
	tune(plain, false, "high", &temp)
	if _, ok := plain["reasoning_effort"]; ok || plain["temperature"] != 0.5 {
		t.Fatalf("обычная модель: %v", plain)
	}
}
