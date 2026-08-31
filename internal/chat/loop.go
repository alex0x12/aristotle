package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// MaxToolIterations — потолок обращений к корпусу за один ход. Без него модель,
// не найдя нужного, уходит перебирать формулировки.
const MaxToolIterations = 4

type Finder interface {
	Search(ctx context.Context, query, group string, k int) ([]corpus.Hit, error)
}

type Session struct {
	Provider provider.Provider
	Search   Finder
	System   string
	Messages []provider.Message
}

type TurnResult struct {
	Text       string
	Chapters   []string
	Iterations int
	Exhausted  bool
}

func argString(input map[string]any, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}

func argInt(input map[string]any, key string, fallback int) int {
	switch v := input[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

// renderHits готовит текст результата инструмента: главы целиком со ссылками.
func renderHits(hits []corpus.Hit, exhausted bool) (string, []string) {
	if len(hits) == 0 {
		return "В корпусе ничего не найдено по этому запросу.", nil
	}
	var b strings.Builder
	texts := make([]string, 0, len(hits))
	for _, h := range hits {
		c := h.Chapter
		fmt.Fprintf(&b, "### %s\n", c.Cite)
		if c.Authenticity == "disputed" {
			b.WriteString("(подлинность этого сочинения оспаривается — сказать об этом)\n")
		}
		b.WriteString(c.Text)
		b.WriteString("\n\n")
		texts = append(texts, c.Text)
	}
	if exhausted {
		b.WriteString("(поиск исчерпан: обращений к корпусу больше не будет, " +
			"отвечай тем, что найдено, либо признай, что не нашёл)\n")
	}
	return strings.TrimSpace(b.String()), texts
}

func (s *Session) Turn(ctx context.Context, userText string,
	onText func(string)) (TurnResult, error) {

	s.Messages = append(s.Messages,
		provider.Message{Role: provider.RoleUser, Text: userText})

	var res TurnResult
	var answer strings.Builder

	for {
		req := provider.Request{
			System:   s.System,
			Messages: s.Messages,
			Tools:    []provider.ToolDef{provider.SearchCorpusTool()},
		}
		stream, err := s.Provider.Stream(ctx, req)
		if err != nil {
			return res, err
		}

		var text strings.Builder
		var calls []provider.ToolCall
		for stream.Next() {
			ev := stream.Event()
			switch ev.Kind {
			case provider.EventText:
				text.WriteString(ev.Text)
				answer.WriteString(ev.Text)
				if onText != nil {
					onText(ev.Text)
				}
			case provider.EventToolCall:
				calls = append(calls, ev.Call)
			}
		}
		if err := stream.Err(); err != nil {
			stream.Close()
			return res, err
		}
		stream.Close()

		s.Messages = append(s.Messages, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      text.String(),
			ToolCalls: calls,
		})

		if len(calls) == 0 {
			res.Text = answer.String()
			return res, nil
		}

		res.Iterations++
		res.Exhausted = res.Iterations >= MaxToolIterations

		results := make([]provider.ToolResult, 0, len(calls))
		for _, call := range calls {
			hits, err := s.Search.Search(ctx,
				argString(call.Input, "query"),
				argString(call.Input, "group"),
				argInt(call.Input, "k", 6))
			if err != nil {
				results = append(results, provider.ToolResult{
					ID: call.ID, IsError: true,
					Content: "поиск не удался: " + err.Error(),
				})
				continue
			}
			content, texts := renderHits(hits, res.Exhausted)
			res.Chapters = append(res.Chapters, texts...)
			results = append(results, provider.ToolResult{
				ID: call.ID, Content: content,
			})
		}
		s.Messages = append(s.Messages, provider.Message{
			Role: provider.RoleUser, ToolResults: results,
		})
	}
}
