package chat

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/alex0x12/aristotle/internal/provider"
)

// DefaultRatio — начальная оценка токенов на символ для смеси русского и
// английского. Уточняется по первому же ответу с известным расходом.
const DefaultRatio = 0.33

// omitted — чем заменяются выдержки прошлых ходов, когда они не влезают.
const omitted = "[выдержки опущены]"

// Trim — что не вошло в запрос: сколько результатов поиска заменено пометкой
// и сколько старых ходов пропущено целиком.
type Trim struct {
	Excerpts int `json:"excerpts"`
	Turns    int `json:"turns"`
}

// Any — было ли урезание вообще.
func (t Trim) Any() bool { return t.Excerpts > 0 || t.Turns > 0 }

// messageChars — размер сообщения в символах, по которому ведётся оценка.
func messageChars(m provider.Message) int {
	n := utf8.RuneCountInString(m.Text)
	for _, c := range m.ToolCalls {
		n += utf8.RuneCountInString(c.Name)
		if b, err := json.Marshal(c.Input); err == nil {
			n += utf8.RuneCount(b)
		}
	}
	for _, r := range m.ToolResults {
		n += utf8.RuneCountInString(r.Content)
	}
	return n
}

func messagesChars(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += messageChars(m)
	}
	return n
}

func toolsChars(tools []provider.ToolDef) int {
	n := 0
	for _, t := range tools {
		n += utf8.RuneCountInString(t.Name) + utf8.RuneCountInString(t.Description)
		if b, err := json.Marshal(t.Schema); err == nil {
			n += utf8.RuneCount(b)
		}
	}
	return n
}

func (s *Session) ratio() float64 {
	if s.tokenRatio > 0 {
		return s.tokenRatio
	}
	return DefaultRatio
}

func (s *Session) estimate(chars int) int { return int(float64(chars)*s.ratio() + 0.5) }

// calibrate уточняет долю токенов на символ по настоящему расходу запроса и
// запоминает, сколько токенов ушло на вход: по расходу, а без него — по оценке.
func (s *Session) calibrate(u provider.Usage, chars int) {
	if u.Known && u.Prompt() > 0 && chars > 0 {
		s.tokenRatio = float64(u.Prompt()) / float64(chars)
		s.LastPrompt = u.Prompt()
		return
	}
	s.LastPrompt = s.estimate(chars)
}

// turnStarts — индексы реплик пользователя с текстом: с них начинаются ходы.
// Результаты инструмента тоже идут с ролью user, но хода не начинают.
func turnStarts(msgs []provider.Message) []int {
	var out []int
	for i, m := range msgs {
		if m.Role == provider.RoleUser && len(m.ToolResults) == 0 {
			out = append(out, i)
		}
	}
	return out
}

// view собирает сообщения запроса так, чтобы оценка уложилась в бюджет.
// fixed — символы системного промпта и инструментов; current — индекс начала
// текущего хода. s.Messages не меняется: журнал и экран видят беседу целиком,
// урезается только то, что уходит модели. fits = false — даже после всех
// урезаний текущий ход не влезает; запрос тогда уходит как есть.
func (s *Session) view(fixed, current int) ([]provider.Message, Trim, bool) {
	msgs := s.Messages
	if s.Budget <= 0 {
		return msgs, Trim{}, true
	}
	total := fixed + messagesChars(msgs)
	if s.estimate(total) <= s.Budget {
		return msgs, Trim{}, true
	}

	// Шаг 1: выдержки прошлых ходов — они самые крупные. Копии, чтобы не
	// тронуть s.Messages; пара вызов — результат остаётся целой, меняется
	// только содержимое результата.
	out := make([]provider.Message, len(msgs))
	copy(out, msgs)
	var trim Trim
	for i := 0; i < current && s.estimate(total) > s.Budget; i++ {
		if len(out[i].ToolResults) == 0 {
			continue
		}
		results := make([]provider.ToolResult, len(out[i].ToolResults))
		copy(results, out[i].ToolResults)
		for j := range results {
			if results[j].Content == omitted {
				continue
			}
			total += utf8.RuneCountInString(omitted) - utf8.RuneCountInString(results[j].Content)
			results[j].Content = omitted
			trim.Excerpts++
		}
		out[i].ToolResults = results
	}
	if s.estimate(total) <= s.Budget {
		return out, trim, true
	}

	// Шаг 2: ходы целиком, от старых к новым. Первый ход остаётся — в нём
	// обычно задан предмет беседы; текущий не трогается никогда. Ход k
	// кончается там, где начинается k+1.
	starts := turnStarts(out)
	dropTo := -1
	for k := 1; k+1 < len(starts) && starts[k+1] <= current && s.estimate(total) > s.Budget; k++ {
		total -= messagesChars(out[starts[k]:starts[k+1]])
		trim.Turns++
		dropTo = starts[k+1]
	}
	if dropTo > 0 {
		kept := make([]provider.Message, 0, len(out)-(dropTo-starts[1]))
		kept = append(kept, out[:starts[1]]...)
		kept = append(kept, out[dropTo:]...)
		out = kept
	}
	return out, trim, s.estimate(total) <= s.Budget
}
