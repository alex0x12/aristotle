// internal/render/ansi.go
package render

import (
	"fmt"
	"io"
)

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	cyan   = "\x1b[36m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
)

// Printer печатает события по мере поступления. Альтернативный экран
// не используется: поток и перерисовка вместе не нужны.
type Printer struct {
	Out   io.Writer
	Color bool

	lastCite string
	lastEn   string
	sources  []string
}

func (p *Printer) paint(color, s string) string {
	if !p.Color {
		return s
	}
	return color + s + reset
}

func (p *Printer) Handle(ev Event, chapters []string) {
	switch ev.Kind {
	case Prose:
		fmt.Fprintln(p.Out, ev.Text)

	case Cite:
		p.lastCite = ev.Text

	case En:
		p.lastEn = ev.Text
		if p.lastCite != "" {
			p.sources = append(p.sources,
				fmt.Sprintf("%s — %s", p.lastCite, ev.Text))
		}

	case Ru:
		mark := ""
		if !Verify(p.lastEn, chapters) {
			mark = " " + p.paint(red, "[!] нет в выдержках")
		}
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(cyan, "│"), ev.Text)
		fmt.Fprintf(p.Out, "%s %s%s\n",
			p.paint(cyan, "│"), p.paint(dim, "— "+p.lastCite), mark)
		p.lastCite, p.lastEn = "", ""

	case Inference:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(yellow, "[вывод]"), ev.Text)

	case FactNote:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(dim, "[факт]"), ev.Text)

	case PremiseNote:
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(dim, "[посылка]"), ev.Text)
	}
}

// Sources — английские оригиналы выдержек последнего ответа, для команды /src.
func (p *Printer) Sources() []string { return p.sources }

func (p *Printer) Reset() {
	p.sources = nil
	p.lastCite, p.lastEn = "", ""
}
