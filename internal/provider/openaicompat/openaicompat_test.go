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
// top_p в запросе нет; режим размышления включён всегда, усилие — наибольшее
// (параметры верхнего уровня, как в документации DeepSeek).
func TestBodyThinkingMaxAndNoTuningKnobs(t *testing.T) {
	body := buildBody("deepseek-flash", provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "х"}},
	}, true)
	for _, key := range []string{"max_tokens", "max_completion_tokens", "temperature", "top_p"} {
		if _, ok := body[key]; ok {
			t.Errorf("в теле запроса есть %q", key)
		}
	}
	th, _ := body["thinking"].(map[string]any)
	if th["type"] != "enabled" || body["reasoning_effort"] != "max" {
		t.Fatalf("режим размышления: thinking=%#v reasoning_effort=%#v", body["thinking"], body["reasoning_effort"])
	}
	if body["model"] != "deepseek-flash" || body["stream"] != true {
		t.Fatalf("тело: %+v", body)
	}
	// Без режима (сервер отказался) параметров нет вовсе.
	plain := buildBody("m", provider.Request{}, false)
	if _, ok := plain["thinking"]; ok {
		t.Error("thinking остался в запросе без режима")
	}
	if _, ok := plain["reasoning_effort"]; ok {
		t.Error("reasoning_effort остался в запросе без режима")
	}
}

// Рассуждение каждого ответа уходит назад при своём сообщении — и внутри
// хода (вместе с вызовом поиска), и в следующих ходах. У сообщения без
// рассуждения (старый журнал) ключа нет вовсе: это принятая серверу форма,
// выдуманное пустое значение — неизвестно как. Без режима размышления
// рассуждений в запросе нет совсем.
func TestBodyReasoningRoundTripAndLegacy(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleUser, Text: "старый вопрос"},
		{Role: provider.RoleAssistant, Text: "старый ответ"}, // из журнала без рассуждения
		{Role: provider.RoleUser, Text: "вопрос"},
		{Role: provider.RoleAssistant, Reasoning: "думаю про поиск",
			ToolCalls: []provider.ToolCall{{ID: "c1", Name: "search_corpus", Input: map[string]any{"query": "x"}}}},
		{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{ID: "c1", Content: "глава"}}},
		{Role: provider.RoleAssistant, Text: "ответ", Reasoning: "итог размышлений"},
	}}
	msgs := buildBody("m", req, true)["messages"].([]map[string]any)
	var assistants []map[string]any
	for _, m := range msgs {
		if m["role"] == "assistant" {
			assistants = append(assistants, m)
		}
	}
	if len(assistants) != 3 {
		t.Fatalf("сообщений ассистента %d", len(assistants))
	}
	if _, has := assistants[0]["reasoning_content"]; has {
		t.Errorf("у старого ответа появился reasoning_content: %+v", assistants[0])
	}
	if assistants[1]["reasoning_content"] != "думаю про поиск" || assistants[1]["tool_calls"] == nil {
		t.Errorf("ответ с вызовом поиска: %+v", assistants[1])
	}
	if assistants[2]["reasoning_content"] != "итог размышлений" || assistants[2]["content"] != "ответ" {
		t.Errorf("итоговый ответ: %+v", assistants[2])
	}
	for _, m := range buildBody("m", req, false)["messages"].([]map[string]any) {
		if _, has := m["reasoning_content"]; has {
			t.Fatalf("рассуждение осталось в запросе без режима: %+v", m)
		}
	}
}

func TestSystemGoesFirstAsMessage(t *testing.T) {
	body := buildBody("gpt-5.1", provider.Request{
		System:   "ты Аристотель",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: "привет"}},
	}, true)
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
	}, true)
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
	for _, key := range []string{"max_tokens", "max_completion_tokens", "temperature"} {
		if _, ok := sent[key]; ok {
			t.Errorf("в запросе есть %q", key)
		}
	}
	if sent["reasoning_effort"] != "max" {
		t.Errorf("reasoning_effort = %v", sent["reasoning_effort"])
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

// events прогоняет записанный поток и возвращает события по порядку.
func events(t *testing.T, sse string) []provider.Event {
	t.Helper()
	s := &stream{body: io.NopCloser(strings.NewReader("")), sc: bufio.NewScanner(strings.NewReader(sse))}
	var out []provider.Event
	for s.Next() {
		out = append(out, s.Event())
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func joined(evs []provider.Event, kind provider.EventKind) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Kind == kind {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

// Порядок рассуждения, текста и вызовов в потоке в документации не описан:
// разбор от него не зависит.
func TestStreamReasoningAnyOrder(t *testing.T) {
	chunk := func(delta string) string {
		return "data: " + `{"choices":[{"index":0,"delta":` + delta + `}]}` + "\n\n"
	}
	cases := map[string]string{
		"рассуждение, потом текст": chunk(`{"reasoning_content":"р1"}`) + chunk(`{"reasoning_content":"р2"}`) +
			chunk(`{"content":"Да"}`),
		"текст, потом рассуждение": chunk(`{"content":"Да"}`) + chunk(`{"reasoning_content":"р1р2"}`),
		"вперемешку в одном куске": chunk(`{"reasoning_content":"р1","content":"Да"}`) + chunk(`{"reasoning_content":"р2"}`),
		"null и пустые куски":      chunk(`{"reasoning_content":null,"content":""}`) + chunk(`{"reasoning_content":"р1р2"}`) + chunk(`{"content":"Да"}`),
	}
	for name, sse := range cases {
		evs := events(t, sse+"data: [DONE]\n\n")
		if got := joined(evs, provider.EventReasoning); got != "р1р2" {
			t.Errorf("%s: рассуждение %q", name, got)
		}
		if got := joined(evs, provider.EventText); got != "Да" {
			t.Errorf("%s: текст %q", name, got)
		}
	}
}

// Рассуждение, потом вызов поиска, расход в куске с finish_reason (форма DeepSeek).
func TestStreamReasoningThenToolCall(t *testing.T) {
	sse := `data: {"choices":[{"index":0,"delta":{"reasoning_content":"надо поискать"}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search_corpus","arguments":"{\"query\":\"mean\"}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":30,"prompt_cache_hit_tokens":40,"prompt_cache_miss_tokens":60}}` + "\n\n" +
		"data: [DONE]\n\n"
	evs := events(t, sse)
	var call provider.ToolCall
	var done provider.Event
	var usage provider.Usage
	for _, e := range evs {
		switch e.Kind {
		case provider.EventToolCall:
			call = e.Call
		case provider.EventDone:
			done = e
		case provider.EventUsage:
			usage = e.Usage
		}
	}
	if joined(evs, provider.EventReasoning) != "надо поискать" || call.Name != "search_corpus" || call.Input["query"] != "mean" {
		t.Fatalf("события: %+v", evs)
	}
	if done.StopReason != "tool_use" || usage != (provider.Usage{Input: 60, Output: 30, CacheRead: 40, Known: true}) {
		t.Fatalf("конец: %+v, расход %+v", done, usage)
	}
}

// ---- отказ сервера от параметров режима размышления ----

func resetRejected() {
	thinkingRejected.Range(func(k, _ any) bool { thinkingRejected.Delete(k); return true })
}

const okSSE = `data: {"choices":[{"index":0,"delta":{"content":"Да."},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"

// fakeServer отвечает по сценарию: статус и тело на каждый запрос по порядку
// (последний повторяется); тела запросов записываются.
type fakeServer struct {
	mu     sync.Mutex
	bodies []map[string]any
	script []struct {
		status int
		body   string
	}
}

func (f *fakeServer) handler(w http.ResponseWriter, r *http.Request) {
	var b map[string]any
	json.NewDecoder(r.Body).Decode(&b)
	f.mu.Lock()
	n := len(f.bodies)
	f.bodies = append(f.bodies, b)
	f.mu.Unlock()
	step := f.script[min(n, len(f.script)-1)]
	w.WriteHeader(step.status)
	io.WriteString(w, step.body)
}

func (f *fakeServer) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.bodies) }

func (f *fakeServer) add(status int, body string) {
	f.script = append(f.script, struct {
		status int
		body   string
	}{status, body})
}

func collect(t *testing.T, c provider.Provider, req provider.Request) []provider.Event {
	t.Helper()
	st, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer st.Close()
	var out []provider.Event
	for st.Next() {
		out = append(out, st.Event())
	}
	return out
}

var reqWithHistory = provider.Request{Messages: []provider.Message{
	{Role: provider.RoleUser, Text: "в"},
	{Role: provider.RoleAssistant, Text: "о", Reasoning: "рассуждение"},
	{Role: provider.RoleUser, Text: "в2"},
}}

// Отказ с названным параметром: ровно один повтор без thinking, reasoning_effort
// и рассуждений в истории; предупреждение — одно на адрес и модель, дальше
// запросы идут без параметров сразу; смена модели пробует параметры снова.
func TestThinkingRejectedRetriesOnceAndRemembers(t *testing.T) {
	resetRejected()
	f := &fakeServer{}
	f.add(400, `{"error":{"message":"Unknown parameter: 'thinking'.","type":"invalid_request_error"}}`)
	f.add(200, okSSE)
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()

	c := New(srv.URL, "m1", "ключ")
	evs := collect(t, c, reqWithHistory)
	if f.count() != 2 {
		t.Fatalf("запросов %d, ожидалось 2 (отказ и повтор)", f.count())
	}
	first, second := f.bodies[0], f.bodies[1]
	if first["thinking"] == nil || first["reasoning_effort"] != "max" {
		t.Errorf("первый запрос без параметров: %+v", first)
	}
	if _, has := second["thinking"]; has {
		t.Error("повтор с thinking")
	}
	if _, has := second["reasoning_effort"]; has {
		t.Error("повтор с reasoning_effort")
	}
	for _, m := range second["messages"].([]any) {
		if _, has := m.(map[string]any)["reasoning_content"]; has {
			t.Errorf("повтор с reasoning_content: %+v", m)
		}
	}
	if len(evs) == 0 || evs[0].Kind != provider.EventNotice || !strings.Contains(evs[0].Text, "thinking") {
		t.Fatalf("первым событием должно быть предупреждение: %+v", evs)
	}
	if joined(evs, provider.EventText) != "Да." {
		t.Errorf("текст повтора потерян: %+v", evs)
	}

	// Следующий запрос: без отказа вперёд и без второго предупреждения.
	f.script = f.script[1:]
	before := f.count()
	evs = collect(t, c, reqWithHistory)
	if f.count() != before+1 {
		t.Errorf("второй запрос ушёл %d раз(а)", f.count()-before)
	}
	if _, has := f.bodies[f.count()-1]["thinking"]; has {
		t.Error("запомненный отказ не учтён: thinking снова в запросе")
	}
	if joined(evs, provider.EventNotice) != "" {
		t.Error("второе предупреждение")
	}

	// Другая модель — другой ключ памяти: параметры пробуются снова.
	f.script = f.script[:0]
	f.add(200, okSSE)
	collect(t, New(srv.URL, "m2", "ключ"), reqWithHistory)
	if f.bodies[f.count()-1]["thinking"] == nil {
		t.Error("для новой модели параметры не отправлены")
	}
}

// Отказ про reasoning_content (ошибка в истории) — не про параметры режима:
// повтора нет, ошибка возвращается. Совпадение — по имени параметра, а не по
// слову «reasoning».
func TestRejectionAboutReasoningContentDoesNotRetry(t *testing.T) {
	resetRejected()
	f := &fakeServer{}
	f.add(400, `{"error":{"message":"Missing reasoning_content for an assistant message."}}`)
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	if _, err := New(srv.URL, "m", "к").Stream(context.Background(), reqWithHistory); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if f.count() != 1 {
		t.Fatalf("отказ только про reasoning_content вызвал повтор: запросов %d", f.count())
	}
}

// Второй отказ на повторе — обычная ошибка, без цикла.
func TestSecondRejectionIsAnErrorNoLoop(t *testing.T) {
	resetRejected()
	f := &fakeServer{}
	f.add(400, `{"error":{"message":"bad param reasoning_effort"}}`)
	f.add(401, `{"error":{"message":"неверный ключ"}}`)
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	_, err := New(srv.URL, "m", "к").Stream(context.Background(), reqWithHistory)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("ошибка повтора: %v", err)
	}
	if f.count() != 2 {
		t.Fatalf("запросов %d, ожидалось 2", f.count())
	}
	if _, known := thinkingRejected.Load(srv.URL + "\x00m"); known {
		t.Error("отказ запомнен, хотя повтор не удался")
	}
}
