package render

import (
	"strings"

	"github.com/alex0x12/aristotle/internal/corpus"
)

// Виды блоков, кроме пометок: у пометок вид — их Kind.String() (вывод,
// факт, посылка).
const (
	BlockProse = "prose"
	BlockQuote = "quote"
)

// Block — законченный кусок ответа для веб-интерфейса и журнала.
type Block struct {
	Kind     string `json:"kind"`
	Text     string `json:"text,omitempty"`
	Cite     string `json:"cite,omitempty"`
	En       string `json:"en,omitempty"`
	Ru       string `json:"ru,omitempty"`
	Verified bool   `json:"verified"`
	// Chapter — путь главы в corpus/ для панели источников; пусто, если
	// главы с такой ссылкой в выдаче хода не было.
	Chapter string `json:"chapter,omitempty"`
}

// Assembler собирает врезку из трёх строк (cite, en, ru) по тем же правилам,
// что Printer: сверка — на en, врезка готова на ru, новая ссылка после en
// снимает сверку.
type Assembler struct {
	cite, en, chapter string
	verified, hasEn   bool
}

func (a *Assembler) reset() { *a = Assembler{} }

func (a *Assembler) Handle(ev Event, chapters []corpus.Chapter) []Block {
	switch ev.Kind {
	case Prose:
		return []Block{{Kind: BlockProse, Text: ev.Text}}
	case Cite:
		if a.hasEn {
			a.verified, a.chapter = false, ""
		}
		a.cite = ev.Text
	case En:
		a.en, a.hasEn = ev.Text, true
		a.verified = VerifyInChapter(ev.Text, a.cite, chapters)
		a.chapter = chapterPath(a.cite, chapters)
	case Ru:
		b := Block{Kind: BlockQuote, Cite: a.cite, En: a.en, Ru: ev.Text,
			Verified: a.verified, Chapter: a.chapter}
		a.reset()
		return []Block{b}
	case Inference, FactNote, PremiseNote:
		return []Block{{Kind: ev.Kind.String(), Text: ev.Text}}
	}
	return nil
}

// Flush отдаёт врезку, оставшуюся без ru (модель не дописала перевод):
// она всё равно показывается — по-английски, с результатом сверки.
func (a *Assembler) Flush() []Block {
	if !a.hasEn {
		a.reset()
		return nil
	}
	b := Block{Kind: BlockQuote, Cite: a.cite, En: a.en, Verified: a.verified, Chapter: a.chapter}
	a.reset()
	return []Block{b}
}

func chapterPath(cite string, chapters []corpus.Chapter) string {
	cite = strings.TrimSpace(cite)
	for _, c := range chapters {
		if strings.TrimSpace(c.Cite) == cite {
			return c.Path
		}
	}
	return ""
}
