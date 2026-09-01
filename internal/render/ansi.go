// internal/render/ansi.go
package render

import (
	"fmt"
	"io"

	"github.com/alex0x12/aristotle/internal/corpus"
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

	lastCite     string
	lastEn       string
	lastVerified bool
	sources      []string
}

func (p *Printer) paint(color, s string) string {
	if !p.Color {
		return s
	}
	return color + s + reset
}

// Handle обрабатывает одно событие потока. chapters — главы, которые поиск
// реально вернул на этом ходу, вместе со ссылками: сверка обязана искать
// выдержку именно в главе с той ссылкой, которую заявила модель.
func (p *Printer) Handle(ev Event, chapters []corpus.Chapter) {
	switch ev.Kind {
	case Prose:
		fmt.Fprintln(p.Out, ev.Text)

	case Cite:
		// Если выдержка уже накоплена (lastEn не пуст) и модель прислала
		// новый адрес до ru, сверка, сделанная на En, устарела: она
		// проверяла текст против СТАРОГО адреса, а печататься он будет
		// под новым. Пересверять здесь нечем — новая ссылка может вообще
		// не входить в chapters, — поэтому честнее всего снять
		// подтверждённость: врезка на Ru получит пометку, вместо того
		// чтобы молча унаследовать результат сверки по чужому адресу.
		if p.lastEn != "" {
			p.lastVerified = false
		}
		p.lastCite = ev.Text

	case En:
		p.lastEn = ev.Text
		// Сверка делается здесь, на En, а не на Ru. Причины две: во-первых,
		// строка обязана попасть в sources (для /src) сразу, с результатом
		// сверки, а не позже — если модель не напишет ru вовсе (обычная
		// оплошность), выдержка иначе не проверилась бы никогда и попала бы
		// в /src как чистый источник. Во-вторых, /src должен показывать
		// пометку [!] так же честно, как врезка.
		p.lastVerified = VerifyInChapter(ev.Text, p.lastCite, chapters)
		// Строка идёт в sources всегда, даже без cite (en без
		// предшествующего cite): иначе такая выдержка видна на экране,
		// на Ru, с пометкой [!] (сверка не может её подтвердить без
		// адреса) — но исчезает из /src, как будто её и не было.
		line := ev.Text
		if p.lastCite != "" {
			line = fmt.Sprintf("%s — %s", p.lastCite, ev.Text)
		}
		if !p.lastVerified {
			line += " " + p.paint(red, "[!]")
		}
		p.sources = append(p.sources, line)

	case Ru:
		// Врезка по-прежнему печатается на Ru; сверка при этом уже сделана
		// на En — здесь только используется её результат.
		mark := ""
		if !p.lastVerified {
			mark = " " + p.paint(red, "[!] нет в выдержках")
		}
		fmt.Fprintf(p.Out, "%s %s\n", p.paint(cyan, "│"), ev.Text)
		fmt.Fprintf(p.Out, "%s %s%s\n",
			p.paint(cyan, "│"), p.paint(dim, "— "+p.lastCite), mark)
		p.lastCite, p.lastEn, p.lastVerified = "", "", false

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
	p.lastCite, p.lastEn, p.lastVerified = "", "", false
}
