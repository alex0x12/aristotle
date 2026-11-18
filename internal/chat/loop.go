package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

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
	Corpus   Finder
	System   string
	Messages []provider.Message

	// Budget — потолок входа запроса в токенах; 0 — без урезания.
	Budget int
	// LastPrompt — сколько токенов ушло на вход последнего запроса: по
	// расходу провайдера, а если он не сообщён — по оценке.
	LastPrompt int
	// tokenRatio — токенов на символ по последнему запросу с известным
	// расходом; 0 — ещё не измерено, берётся DefaultRatio.
	tokenRatio float64

	// OnSearchWarning — необязательный колбэк для предупреждений, которые
	// не должны срывать ход, но обязаны дойти до пользователя, а не
	// затеряться в тексте, отданном модели. Пример — отказ поиска: qmd нет
	// в PATH или коллекция не проиндексирована. Модель в этом случае
	// вырождает ход в регистр вывода — правильный ответ на честно пустой
	// корпус (5.5) — и со стороны это неотличимо от честной пустоты, если
	// пользователь не увидит предупреждение отдельно.
	OnSearchWarning func(msg string)

	// OnSearchStart — необязательный колбэк, вызываемый перед каждым
	// обращением к поиску. Холодный qmd поднимается 8-13 секунд, и без
	// какого-либо признака работы это неотличимо от зависания.
	OnSearchStart func(query, group string)
}

type TurnResult struct {
	Text string
	// Chapters — главы, которые поиск реально вернул на этом ходу, вместе
	// с их ссылками. Пара, а не голый текст: сверка обязана проверять, что
	// выдержка нашлась именно в главе с той ссылкой, которую заявила модель,
	// а не в какой-нибудь из пришедших.
	Chapters   []corpus.Chapter
	Iterations int
	Exhausted  bool
	// Usage — расход всех запросов хода (обращения к поиску — отдельные
	// запросы). Known — только если каждый запрос сообщил свой расход.
	Usage provider.Usage
	// Trim — что не вошло в последний запрос хода. Сообщения хода только
	// прибавляются, поэтому урезание последнего запроса — самое сильное.
	Trim Trim
	// OverBudget — запрос не уложился в бюджет и после всех урезаний
	// (системный промпт, инструменты, первый и текущий ход): он ушёл в
	// урезанном виде, а не заблокирован.
	OverBudget bool
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

// exhaustedNote — пометка модели о том, что обращений к корпусу больше не
// будет. Обязана дойти независимо от того, пуста ли последняя выдача:
// потолок часто достигается именно потому, что предыдущие попытки не
// устроили модель, и последний поиск на нём вполне может оказаться пустым.
const exhaustedNote = "(поиск исчерпан: обращений к корпусу больше не будет, " +
	"отвечай тем, что найдено, либо признай, что не нашёл)"

// renderHits готовит текст результата инструмента: главы целиком со ссылками.
func renderHits(hits []corpus.Hit, exhausted bool) (string, []corpus.Chapter) {
	if len(hits) == 0 {
		msg := "В корпусе ничего не найдено по этому запросу."
		if exhausted {
			msg += "\n\n" + exhaustedNote
		}
		return msg, nil
	}
	var b strings.Builder
	chapters := make([]corpus.Chapter, 0, len(hits))
	for _, h := range hits {
		c := h.Chapter
		fmt.Fprintf(&b, "### %s\n", c.Cite)
		if c.Authenticity == "disputed" {
			b.WriteString("(подлинность этого сочинения оспаривается — сказать об этом)\n")
		}
		b.WriteString(c.Text)
		b.WriteString("\n\n")
		chapters = append(chapters, c)
	}
	if exhausted {
		b.WriteString(exhaustedNote + "\n")
	}
	return strings.TrimSpace(b.String()), chapters
}

// Turn проводит один ход диалога. onText получает дельты текста по мере
// поступления от провайдера — вызывающая сторона обязана печатать их сразу,
// не дожидаясь конца хода: холодный провайдер и до четырёх обращений к
// поиску легко растягивают ход до минуты, и пустой экран всё это время
// неотличим от зависания.
//
// onChapters (может быть nil) вызывается сразу после каждого обращения к
// поиску с полным списком глав, накопленным к этому моменту. Смысл в том,
// что главы известны раньше, чем модель напишет первую выдержку: поиск в
// этой архитектуре всегда завершается до начала генерации ответа. Значит
// вызывающая сторона может печатать врезки (cite/en/ru) по мере поступления
// текста ответа, сверяя их с уже готовым списком глав, а не только после
// того, как весь ход закончится.
func (s *Session) Turn(ctx context.Context, userText string,
	onText func(string), onChapters func([]corpus.Chapter)) (TurnResult, error) {

	// baseLen — длина истории до этого хода. Неудачный ход не должен
	// оставлять следов: если провайдер откажет, история откатывается сюда,
	// иначе следующий ход допишет вторую пользовательскую реплику подряд, а
	// это API отвергает.
	baseLen := len(s.Messages)
	s.Messages = append(s.Messages,
		provider.Message{Role: provider.RoleUser, Text: userText})

	var res TurnResult
	var answer strings.Builder
	requests := 0

	for {
		// Потолок обеспечивается устройством, а не только текстовой
		// пометкой: как только он достигнут, инструмент физически не
		// предлагается, и модель не может вызвать его снова.
		var tools []provider.ToolDef
		if res.Iterations < MaxToolIterations {
			tools = []provider.ToolDef{provider.SearchCorpusTool()}
		}
		fixed := utf8.RuneCountInString(s.System) + toolsChars(tools)
		msgs, trim, fits := s.view(fixed, baseLen)
		res.Trim, res.OverBudget = trim, !fits
		reqChars := fixed + messagesChars(msgs)

		req := provider.Request{System: s.System, Messages: msgs, Tools: tools}
		stream, err := s.Provider.Stream(ctx, req)
		if err != nil {
			s.Messages = s.Messages[:baseLen]
			// Расход упавшего запроса неизвестен: уже набранная сумма
			// остаётся в res, но помечается неполной, а не выдаётся за итог.
			res.Usage.Known = false
			return res, err
		}

		var text strings.Builder
		var calls []provider.ToolCall
		var reqUsage provider.Usage
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
			case provider.EventUsage:
				reqUsage = ev.Usage
			}
		}
		if err := stream.Err(); err != nil {
			stream.Close()
			s.Messages = s.Messages[:baseLen]
			res.Usage.Known = false // см. выше: расход оборванного запроса неизвестен
			return res, err
		}
		stream.Close()

		if requests == 0 {
			res.Usage = reqUsage
		} else {
			res.Usage = res.Usage.Add(reqUsage)
		}
		requests++
		s.calibrate(reqUsage, reqChars)

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
			query := argString(call.Input, "query")
			group := argString(call.Input, "group")
			if s.OnSearchStart != nil {
				s.OnSearchStart(query, group)
			}
			hits, err := s.Corpus.Search(ctx, query, group,
				argInt(call.Input, "k", 6))
			if err != nil {
				msg := "поиск не удался: " + err.Error()
				if s.OnSearchWarning != nil {
					s.OnSearchWarning(msg)
				}
				results = append(results, provider.ToolResult{
					ID: call.ID, IsError: true,
					Content: msg,
				})
				continue
			}
			content, chapters := renderHits(hits, res.Exhausted)
			res.Chapters = append(res.Chapters, chapters...)
			if onChapters != nil {
				onChapters(res.Chapters)
			}
			results = append(results, provider.ToolResult{
				ID: call.ID, Content: content,
			})
		}
		s.Messages = append(s.Messages, provider.Message{
			Role: provider.RoleUser, ToolResults: results,
		})
	}
}
