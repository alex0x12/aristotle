package web

import (
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/render"
)

// blockView — блок ответа на проводе: render.Block плюс русское название
// работы для показа. Обёртка, а не поля в самом Block: теги Block — контракт
// потока и журнала, а русское название вычисляется при отправке и в журнал не
// пишется (врезки там остаются английскими, сверка идёт по ним).
// Ключей нет, когда переводить нечего — интерфейс тогда показывает cite как есть.
type blockView struct {
	render.Block
	CiteRu string `json:"cite_ru,omitempty"` // «Никомахова этика II.1»
	WorkRu string `json:"work_ru,omitempty"` // «Никомахова этика»; нумерация — остаток cite_ru после него
}

// blockForWire — блок для провода. cite_ru и work_ru берутся из одного разбора ссылки,
// чтобы cite_ru всегда начинался с work_ru и пробела: на этом интерфейс делит
// их, не гадая.
func blockForWire(b render.Block) blockView {
	v := blockView{Block: b}
	if b.Kind == render.BlockQuote {
		if ru, num, ok := corpus.SplitCiteRu(b.Cite); ok {
			v.WorkRu, v.CiteRu = ru, ru+" "+num
		}
	}
	return v
}

func blocksForWire(bs []render.Block) []blockView {
	if bs == nil {
		return nil
	}
	out := make([]blockView, len(bs))
	for i, b := range bs {
		out[i] = blockForWire(b)
	}
	return out
}
