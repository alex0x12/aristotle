// internal/render/markers.go
package render

import "strings"

type Kind int

const (
	Prose Kind = iota
	Cite
	En
	Ru
	Inference
	FactNote
	PremiseNote
)

func (k Kind) String() string {
	switch k {
	case Cite:
		return "cite"
	case En:
		return "en"
	case Ru:
		return "ru"
	case Inference:
		return "вывод"
	case FactNote:
		return "факт"
	case PremiseNote:
		return "посылка"
	default:
		return "проза"
	}
}

type Event struct {
	Kind Kind
	Text string
}

// markers — закрытый набор. Порядок важен только для читаемости.
var markers = []struct {
	prefix string
	kind   Kind
}{
	{">> cite:", Cite},
	{">> en:", En},
	{">> ru:", Ru},
	{">> вывод:", Inference},
	{">> [факт]:", FactNote},
	{">> [посылка]:", PremiseNote},
}

// Parser превращает поток текстовых дельт в поток строчных событий.
// Маркеры строчные, поэтому копим до перевода строки.
type Parser struct {
	buf strings.Builder
}

func classify(line string) Event {
	for _, m := range markers {
		if strings.HasPrefix(line, m.prefix) {
			return Event{
				Kind: m.kind,
				Text: strings.TrimSpace(strings.TrimPrefix(line, m.prefix)),
			}
		}
	}
	return Event{Kind: Prose, Text: line}
}

func (p *Parser) Write(delta string) []Event {
	p.buf.WriteString(delta)
	s := p.buf.String()

	var events []Event
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		events = append(events, classify(strings.TrimRight(s[:i], "\r")))
		s = s[i+1:]
	}

	p.buf.Reset()
	p.buf.WriteString(s)
	return events
}

// Flush отдаёт остаток без перевода строки. Повторный вызов пуст.
func (p *Parser) Flush() []Event {
	rest := p.buf.String()
	p.buf.Reset()
	if rest == "" {
		return nil
	}
	return []Event{classify(rest)}
}
