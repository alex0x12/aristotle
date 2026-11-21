// Package opencodecli — провайдер, который зовёт установленный opencode
// (`opencode run`) вместо HTTP-шлюза. Нужен для бесплатных моделей OpenCode:
// их прямой OpenAI-совместимый адрес отдаёт FreeUsageLimitError, а CLI из той
// же машины отвечает.
//
// Модель в приложении умеет только один инструмент — search_corpus. Провайдер
// не гоняет его через opencode: первый Stream отдаёт готовый ToolCall, поиск
// выполняет само приложение (и получает главы для сверки цитат), а второй
// Stream уже зовёт opencode с выдержками и просит ответ по правилам персоны.
package opencodecli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alex0x12/aristotle/internal/provider"
)

type client struct {
	model  string
	effort string
	// command — подмена exec.CommandContext в тестах; nil — настоящий opencode.
	command func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// New — провайдер для модели OpenCode (id без префикса opencode/).
func New(model, effort string) provider.Provider {
	return &client{model: model, effort: effort, command: exec.CommandContext}
}

func (c *client) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	if needsSearch(req.Messages) {
		query := lastUserText(req.Messages)
		if strings.TrimSpace(query) == "" {
			query = "философия"
		}
		return &oneShot{events: []provider.Event{
			{Kind: provider.EventToolCall, Call: provider.ToolCall{
				ID: "search_1", Name: "search_corpus",
				Input: map[string]any{"query": query},
			}},
			{Kind: provider.EventUsage, Usage: provider.Usage{Known: true}},
			{Kind: provider.EventDone, StopReason: "tool_calls"},
		}}, nil
	}

	prompt := buildPrompt(req)
	args := []string{"run", "-m", modelArg(c.model), "--format", "json",
		"--title", "aristotle-chat", prompt}
	cmd := c.command(ctx, "opencode", args...)
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("opencode run: %w", err)
	}
	return &stream{cmd: cmd, sc: bufio.NewScanner(stdout), stderr: &stderr}, nil
}

func modelArg(model string) string {
	if strings.HasPrefix(model, "opencode/") {
		return model
	}
	return "opencode/" + model
}

// needsSearch — первый ли это запрос текущего хода. В истории прошлых ходов
// результаты инструмента уже есть, и по ним нельзя решать: нужен результат
// после последней настоящей реплики пользователя.
func needsSearch(msgs []provider.Message) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != provider.RoleUser || len(msgs[i].ToolResults) > 0 {
			continue
		}
		for j := i + 1; j < len(msgs); j++ {
			if len(msgs[j].ToolResults) > 0 {
				return false
			}
		}
		return true
	}
	return true
}

func lastUserText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser && len(msgs[i].ToolResults) == 0 {
			return strings.TrimSpace(msgs[i].Text)
		}
	}
	return ""
}

// buildPrompt склеивает системные правила, историю и выдержки в один запрос:
// opencode run — один ход CLI, своего состояния между вызовами у нас нет.
func buildPrompt(req provider.Request) string {
	var b strings.Builder
	if req.System != "" {
		b.WriteString(req.System)
		b.WriteString("\n\n---\n\n")
	}
	b.WriteString("Ниже — беседа и выдержки из корпуса. Не вызывай инструменты и не читай файлы: ")
	b.WriteString("отвечай только по этим выдержкам. Если выдержек нет — скажи, что в корпусе этого нет.\n\n")
	for _, m := range req.Messages {
		for _, res := range m.ToolResults {
			b.WriteString("ВЫДЕРЖКИ ИЗ КОРПУСА:\n")
			b.WriteString(res.Content)
			b.WriteString("\n\n")
		}
		switch m.Role {
		case provider.RoleUser:
			if strings.TrimSpace(m.Text) != "" {
				b.WriteString("СОБЕСЕДНИК: ")
				b.WriteString(m.Text)
				b.WriteString("\n\n")
			}
		case provider.RoleAssistant:
			if strings.TrimSpace(m.Text) != "" {
				b.WriteString("ТВОЙ ПРЕЖНИЙ ОТВЕТ: ")
				b.WriteString(m.Text)
				b.WriteString("\n\n")
			}
		}
	}
	b.WriteString("Ответь на последний вопрос, соблюдая правила ответа и маркеры (>> cite:, >> en:, >> ru:).")
	return b.String()
}

type jsonPart struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Tokens *struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cache  struct {
			Read int `json:"read"`
		} `json:"cache"`
	} `json:"tokens"`
}

type jsonEvent struct {
	Type string   `json:"type"`
	Part jsonPart `json:"part"`
}

type oneShot struct {
	events []provider.Event
	cur    provider.Event
	err    error
}

func (s *oneShot) Next() bool {
	if len(s.events) == 0 {
		return false
	}
	s.cur, s.events = s.events[0], s.events[1:]
	return true
}
func (s *oneShot) Event() provider.Event { return s.cur }
func (s *oneShot) Err() error            { return s.err }
func (s *oneShot) Close() error          { return nil }

type stream struct {
	cmd    *exec.Cmd
	sc     *bufio.Scanner
	stderr *bytes.Buffer
	cur    provider.Event
	done   bool
	err    error
	waited bool
	mu     sync.Mutex
}

func (s *stream) Next() bool {
	if s.done {
		return false
	}
	for s.sc.Scan() {
		var ev jsonEvent
		if err := json.Unmarshal(s.sc.Bytes(), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "text":
			if ev.Part.Text != "" {
				s.cur = provider.Event{Kind: provider.EventText, Text: ev.Part.Text}
				return true
			}
		case "step_finish":
			if ev.Part.Tokens != nil {
				s.cur = provider.Event{Kind: provider.EventUsage, Usage: provider.Usage{
					Input: ev.Part.Tokens.Input, Output: ev.Part.Tokens.Output,
					CacheRead: ev.Part.Tokens.Cache.Read, Known: true,
				}}
				return true
			}
		}
	}
	s.finish()
	return false
}

func (s *stream) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waited {
		return
	}
	s.waited = true
	if err := s.cmd.Wait(); err != nil && s.err == nil {
		msg := strings.TrimSpace(s.stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		s.err = fmt.Errorf("opencode run: %s", msg)
	}
	s.done = true
}

func (s *stream) Event() provider.Event { return s.cur }
func (s *stream) Err() error            { return s.err }
func (s *stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.done = true
	return nil
}
