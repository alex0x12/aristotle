package openaicompat

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// Настраиваемы только адрес, модель и ключ: потолка вывода, температуры и
// усилия рассуждения в запросе нет (ни под какими из прежних имён).
func TestBodyHasNoTuningKnobs(t *testing.T) {
	body := buildBody("deepseek-flash", provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	for _, key := range []string{"max_tokens", "max_completion_tokens", "temperature", "reasoning_effort", "reasoning", "top_p"} {
		if _, ok := body[key]; ok {
			t.Errorf("в теле запроса есть %q", key)
		}
	}
	if body["model"] != "deepseek-flash" || body["stream"] != true {
		t.Fatalf("тело: %+v", body)
	}
}

func TestSystemGoesFirstAsMessage(t *testing.T) {
	body := buildBody("gpt-5.1", provider.Request{
		System:   "ты Аристотель",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "привет"}},
	})
	msgs := body["messages"].([]map[string]any)
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "ты Аристотель" {
		t.Fatalf("первое сообщение: %+v", msgs[0])
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
	body := buildBody("deepseek-v4-pro", provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	opts, ok := body["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Fatalf("stream_options = %#v", body["stream_options"])
	}
}

// serve поднимает фальшивый провайдер: запоминает запрос, отдаёт заданный
// ответ. В сеть дальше loopback ничего не уходит.
func serve(t *testing.T, status int, body string) (url string, got func() (*http.Request, []byte)) {
	t.Helper()
	var mu sync.Mutex
	var last *http.Request
	var lastBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		last, lastBody = r, b
		mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() (*http.Request, []byte) {
		mu.Lock()
		defer mu.Unlock()
		return last, lastBody
	}
}

func TestStreamSendsConfiguredModelKeyAndAddress(t *testing.T) {
	url, got := serve(t, 200, deepseekStream)
	// Хвостовой слэш адреса не должен давать «//chat/completions».
	p := New(url+"/", "my-model", "FAKE-KEY-0000")
	st, err := p.Stream(context.Background(), provider.Request{
		System:   "ты Аристотель",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "привет"}},
		Tools:    []provider.ToolDef{provider.SearchCorpusTool()},
	})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var usage provider.Usage
	for st.Next() {
		switch ev := st.Event(); ev.Kind {
		case provider.EventText:
			text.WriteString(ev.Text)
		case provider.EventUsage:
			usage = ev.Usage
		}
	}
	st.Close()
	if text.String() != "Да." || !usage.Known {
		t.Fatalf("текст %q, расход %+v", text.String(), usage)
	}
	req, body := got()
	if req.Method != "POST" || req.URL.Path != "/chat/completions" {
		t.Fatalf("запрос %s %s", req.Method, req.URL.Path)
	}
	if h := req.Header.Get("Authorization"); h != "Bearer FAKE-KEY-0000" {
		t.Fatalf("Authorization = %q", h)
	}
	if req.Header.Get("X-Api-Key") != "" {
		t.Fatal("лишний заголовок с ключом")
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["model"] != "my-model" {
		t.Fatalf("model = %v", sent["model"])
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "temperature", "reasoning_effort"} {
		if _, ok := sent[key]; ok {
			t.Errorf("в запросе есть %q", key)
		}
	}
}

// Отказ провайдера доходит до пользователя, но ключ из него вычищен: некоторые
// провайдеры повторяют присланный ключ в тексте ошибки.
func TestStreamErrorDoesNotRepeatKey(t *testing.T) {
	url, _ := serve(t, 401, `{"error":"Incorrect API key provided: FAKE-KEY-0000 is invalid"}`)
	_, err := New(url, "m", "FAKE-KEY-0000").Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if strings.Contains(err.Error(), "FAKE-KEY-0000") {
		t.Fatalf("ошибка повторяет ключ: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("ошибка потеряла статус: %v", err)
	}
}
