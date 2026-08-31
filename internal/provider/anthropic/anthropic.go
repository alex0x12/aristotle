package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/provider"
)

type client struct {
	api   sdk.Client
	model string
}

func New(p config.Profile) (provider.Provider, error) {
	key, err := p.APIKey()
	if err != nil {
		return nil, err
	}
	return &client{
		api:   sdk.NewClient(option.WithAPIKey(key)),
		model: p.Model,
	}, nil
}

func (c *client) params(req provider.Request) (sdk.MessageNewParams, error) {
	p := sdk.MessageNewParams{
		Model:     sdk.Model(c.model),
		MaxTokens: int64(req.EffectiveMaxTokens()),
	}

	// Кэшируем префикс: персона не меняется в течение беседы.
	if req.System != "" {
		p.System = []sdk.TextBlockParam{{
			Text:         req.System,
			CacheControl: sdk.NewCacheControlEphemeralParam(),
		}}
	}

	for _, t := range req.Tools {
		required, ok := requiredFields(t.Schema)
		if !ok {
			return sdk.MessageNewParams{}, fmt.Errorf(
				"инструмент %s: поле required в схеме имеет тип %T, ожидался []string или []any из строк",
				t.Name, t.Schema["required"])
		}
		p.Tools = append(p.Tools, sdk.ToolUnionParam{
			OfTool: &sdk.ToolParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				InputSchema: sdk.ToolInputSchemaParam{
					Properties: t.Schema["properties"],
					Required:   required,
				},
			},
		})
	}

	for _, m := range req.Messages {
		var blocks []sdk.ContentBlockParamUnion
		if m.Text != "" {
			blocks = append(blocks, sdk.NewTextBlock(m.Text))
		}
		for _, call := range m.ToolCalls {
			blocks = append(blocks, sdk.NewToolUseBlock(call.ID, call.Input, call.Name))
		}
		for _, res := range m.ToolResults {
			blocks = append(blocks, sdk.NewToolResultBlock(res.ID, res.Content, res.IsError))
		}
		if m.Role == provider.RoleAssistant {
			p.Messages = append(p.Messages, sdk.NewAssistantMessage(blocks...))
		} else {
			p.Messages = append(p.Messages, sdk.NewUserMessage(blocks...))
		}
	}
	return p, nil
}

// requiredFields извлекает список "required" из схемы инструмента.
//
// Схема принимает две равноправные формы: [] string, которую кладёт наш
// собственный SearchCorpusTool(), и []any из строк — форму, в которую
// encoding/json разворачивает JSON-массив, если схема когда-нибудь придёт
// извне (из конфига или сети). Обе формы — законный ввод.
//
// ok=false означает не отсутствие ограничения, а то, что поле "required"
// присутствует, но не приводится ни к одной из этих форм — то есть ошибку
// нашего собственного кода, которая обязана быть замечена, а не проглочена
// молча вместе с ограничением на аргументы инструмента.
func requiredFields(schema map[string]any) (fields []string, ok bool) {
	raw, present := schema["required"]
	if !present {
		return nil, true
	}
	switch v := raw.(type) {
	case []string:
		return v, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, isString := item.(string)
			if !isString {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func (c *client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}
	raw := c.api.Messages.NewStreaming(ctx, params)
	return &stream{raw: raw}, nil
}

type stream struct {
	raw     *ssestream.Stream[sdk.MessageStreamEventUnion]
	msg     sdk.Message
	pending []provider.Event
	cur     provider.Event
	done    bool
	err     error
}

func (s *stream) Next() bool {
	if len(s.pending) > 0 {
		s.cur, s.pending = s.pending[0], s.pending[1:]
		return true
	}
	for s.raw.Next() {
		ev := s.raw.Current()
		if err := s.msg.Accumulate(ev); err != nil {
			s.err = err
			return false
		}
		if delta, ok := ev.AsAny().(sdk.ContentBlockDeltaEvent); ok {
			if td, ok := delta.Delta.AsAny().(sdk.TextDelta); ok && td.Text != "" {
				s.cur = provider.Event{Kind: provider.EventText, Text: td.Text}
				return true
			}
		}
	}
	if err := s.raw.Err(); err != nil {
		s.err = err
		return false
	}
	if s.done {
		return false
	}
	s.done = true
	s.pending = s.finish()
	if len(s.pending) == 0 {
		return false
	}
	s.cur, s.pending = s.pending[0], s.pending[1:]
	return true
}

// finish собирает вызовы инструментов из накопленного сообщения.
func (s *stream) finish() []provider.Event {
	var out []provider.Event
	for _, block := range s.msg.Content {
		use, ok := block.AsAny().(sdk.ToolUseBlock)
		if !ok {
			continue
		}
		var input map[string]any
		// Модель может по-разному экранировать JSON — всегда разбираем,
		// никогда не сравниваем сериализованную строку. use.Input уже
		// является сырым JSON (json.RawMessage), разбираем его напрямую.
		if err := json.Unmarshal(use.Input, &input); err != nil {
			s.err = fmt.Errorf("разбор аргументов инструмента: %w", err)
			return nil
		}
		out = append(out, provider.Event{
			Kind: provider.EventToolCall,
			Call: provider.ToolCall{ID: use.ID, Name: use.Name, Input: input},
		})
	}
	return append(out, provider.Event{
		Kind:       provider.EventDone,
		StopReason: string(s.msg.StopReason),
	})
}

func (s *stream) Event() provider.Event { return s.cur }
func (s *stream) Err() error            { return s.err }
func (s *stream) Close() error          { return s.raw.Close() }
