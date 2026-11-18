package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
)

// accumulator склеивает вызовы функций из потока. Ключ — index, потому что
// провайдер вправе чередовать куски разных вызовов.
type accumulator struct {
	byIndex map[int]*partial
}

type partial struct {
	id, name string
	args     strings.Builder
}

func (a *accumulator) Add(index int, id, name, fragment string) {
	if a.byIndex == nil {
		a.byIndex = map[int]*partial{}
	}
	p, ok := a.byIndex[index]
	if !ok {
		p = &partial{}
		a.byIndex[index] = p
	}
	if id != "" {
		p.id = id
	}
	if name != "" {
		p.name = name
	}
	p.args.WriteString(fragment)
}

func (a *accumulator) Calls() []provider.ToolCall {
	indexes := make([]int, 0, len(a.byIndex))
	for i := range a.byIndex {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)

	var out []provider.ToolCall
	for _, i := range indexes {
		p := a.byIndex[i]
		var input map[string]any
		if err := json.Unmarshal([]byte(p.args.String()), &input); err != nil {
			continue // недособранный или битый вызов пропускаем, не роняя ход
		}
		out = append(out, provider.ToolCall{ID: p.id, Name: p.name, Input: input})
	}
	return out
}

func buildBody(model string, reasoning bool, maxTokens int, req provider.Request) map[string]any {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		for _, res := range m.ToolResults {
			msgs = append(msgs, map[string]any{
				"role": "tool", "tool_call_id": res.ID, "content": res.Content,
			})
		}
		if m.Text == "" && len(m.ToolCalls) == 0 {
			continue
		}
		msg := map[string]any{"role": string(m.Role), "content": m.Text}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, c := range m.ToolCalls {
				args, _ := json.Marshal(c.Input)
				calls = append(calls, map[string]any{
					"id": c.ID, "type": "function",
					"function": map[string]any{"name": c.Name, "arguments": string(args)},
				})
			}
			msg["tool_calls"] = calls
		}
		msgs = append(msgs, msg)
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   true,
		// Без этого поля OpenAI и OpenRouter не присылают расход вовсе.
		"stream_options": map[string]any{"include_usage": true},
	}
	if reasoning {
		body["max_completion_tokens"] = req.EffectiveMaxTokensFor(maxTokens)
	} else {
		body["max_tokens"] = req.EffectiveMaxTokensFor(maxTokens)
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": t.Name, "description": t.Description,
					"parameters": t.Schema,
				},
			})
		}
		body["tools"] = tools
	}
	return body
}

// tune дописывает настройки профиля, которых нет в запросе: усилие
// рассуждения — только рассуждающим моделям, температуру — только обычным
// (рассуждающие её не принимают).
func tune(body map[string]any, reasoning bool, effort string, temperature *float64) {
	if reasoning && effort != "" {
		body["reasoning_effort"] = effort
	}
	if !reasoning && temperature != nil {
		body["temperature"] = *temperature
	}
}

type client struct {
	baseURL     string
	key         string
	model       string
	reasoning   bool
	maxTokens   int
	effort      string
	temperature *float64
	http        *http.Client
}

func New(p config.Profile) (provider.Provider, error) {
	key, err := p.APIKey()
	if err != nil {
		return nil, err
	}
	return &client{
		baseURL:     strings.TrimSuffix(p.BaseURL, "/"),
		key:         key,
		model:       p.Model,
		reasoning:   p.Reasoning,
		maxTokens:   p.MaxOutputTokens,
		effort:      p.ReasoningEffort,
		temperature: p.Temperature,
		http:        &http.Client{},
	}, nil
}

func (c *client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	body := buildBody(c.model, c.reasoning, c.maxTokens, req)
	tune(body, c.reasoning, c.effort, c.temperature)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(buf.String()))
	}
	return &stream{body: resp.Body, sc: bufio.NewScanner(resp.Body)}, nil
}

// chunkUsage — расход в потоке. Кэш у провайдеров назван по-разному:
// OpenAI и OpenRouter — prompt_tokens_details.cached_tokens, DeepSeek —
// prompt_cache_hit_tokens.
type chunkUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	PromptCacheHitTokens *int `json:"prompt_cache_hit_tokens"`
}

func usageOf(u chunkUsage) provider.Usage {
	cached := 0
	switch {
	case u.PromptCacheHitTokens != nil:
		cached = *u.PromptCacheHitTokens
	case u.PromptTokensDetails != nil:
		cached = u.PromptTokensDetails.CachedTokens
	}
	return provider.Usage{
		Input:     u.PromptTokens - cached,
		Output:    u.CompletionTokens,
		CacheRead: cached,
		Known:     true,
	}
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chunkUsage `json:"usage"`
}

type stream struct {
	body    interface{ Close() error }
	sc      *bufio.Scanner
	acc     accumulator
	pending []provider.Event
	cur     provider.Event
	stop    string
	usage   provider.Usage
	done    bool
	err     error
}

func (s *stream) Next() bool {
	if len(s.pending) > 0 {
		s.cur, s.pending = s.pending[0], s.pending[1:]
		return true
	}
	for s.sc.Scan() {
		line := strings.TrimSpace(s.sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ch chunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue
		}
		// Расход приходит последним куском, у OpenAI — с пустым choices.
		// Разбирать его надо до проверки choices, иначе он пропадёт.
		if ch.Usage != nil {
			s.usage = usageOf(*ch.Usage)
		}
		if len(ch.Choices) == 0 {
			continue
		}
		choice := ch.Choices[0]
		if choice.FinishReason != "" {
			s.stop = choice.FinishReason
		}
		for _, tc := range choice.Delta.ToolCalls {
			s.acc.Add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if choice.Delta.Content != "" {
			s.cur = provider.Event{Kind: provider.EventText, Text: choice.Delta.Content}
			return true
		}
	}
	if err := s.sc.Err(); err != nil {
		s.err = err
		return false
	}
	if s.done {
		return false
	}
	s.done = true
	for _, call := range s.acc.Calls() {
		s.pending = append(s.pending,
			provider.Event{Kind: provider.EventToolCall, Call: call})
	}
	stop := s.stop
	if stop == "tool_calls" {
		stop = "tool_use" // приводим к словарю нейтрального слоя
	}
	s.pending = append(s.pending, provider.Event{Kind: provider.EventUsage, Usage: s.usage})
	s.pending = append(s.pending,
		provider.Event{Kind: provider.EventDone, StopReason: stop})
	s.cur, s.pending = s.pending[0], s.pending[1:]
	return true
}

func (s *stream) Event() provider.Event { return s.cur }
func (s *stream) Err() error            { return s.err }
func (s *stream) Close() error          { return s.body.Close() }
