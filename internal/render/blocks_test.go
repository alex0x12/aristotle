package render

import (
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

var ethics = corpus.Chapter{
	Cite: "Nicomachean Ethics II.1",
	Path: "nicomachean-ethics/ii/01.md",
	Text: "Virtue, then, being of two kinds, intellectual and moral, intellectual virtue in the main owes both its birth and its growth to teaching, while moral virtue comes about as a result of habit, whence also its name is formed.",
}

func feed(t *testing.T, chapters []corpus.Chapter, text string) []Block {
	t.Helper()
	p := &Parser{}
	a := &Assembler{}
	var out []Block
	for _, ev := range p.Write(text) {
		out = append(out, a.Handle(ev, chapters)...)
	}
	for _, ev := range p.Flush() {
		out = append(out, a.Handle(ev, chapters)...)
	}
	return append(out, a.Flush()...)
}

func TestAssemblerVerifiedQuote(t *testing.T) {
	got := feed(t, []corpus.Chapter{ethics}, "Добродетель надвое.\n"+
		">> cite: Nicomachean Ethics II.1\n"+
		">> en: moral virtue comes about as a result of habit\n"+
		">> ru: нравственная добродетель возникает как следствие привычки\n")
	if len(got) != 2 || got[0].Kind != BlockProse || got[0].Text != "Добродетель надвое." {
		t.Fatalf("блоки = %+v", got)
	}
	q := got[1]
	if q.Kind != BlockQuote || !q.Verified || q.Chapter != ethics.Path ||
		q.Ru != "нравственная добродетель возникает как следствие привычки" {
		t.Fatalf("врезка = %+v", q)
	}
}

func TestAssemblerUnverifiedQuote(t *testing.T) {
	got := feed(t, []corpus.Chapter{ethics}, ">> cite: Nicomachean Ethics II.1\n"+
		">> en: virtue is a winged chariot of the gods\n>> ru: добродетель — крылатая колесница\n")
	if len(got) != 1 || got[0].Verified {
		t.Fatalf("выдуманная цитата не может быть подтверждена: %+v", got)
	}
}

func TestAssemblerCiteAfterEnDropsVerification(t *testing.T) {
	got := feed(t, []corpus.Chapter{ethics}, ">> cite: Nicomachean Ethics II.1\n"+
		">> en: moral virtue comes about as a result of habit\n"+
		">> cite: Politics I.2\n>> ru: нравственная добродетель — от привычки\n")
	if len(got) != 1 || got[0].Verified || got[0].Cite != "Politics I.2" {
		t.Fatalf("новая ссылка после en снимает сверку, как в Printer: %+v", got)
	}
}

func TestAssemblerFlushesQuoteWithoutRu(t *testing.T) {
	got := feed(t, []corpus.Chapter{ethics}, ">> cite: Nicomachean Ethics II.1\n"+
		">> en: moral virtue comes about as a result of habit\n")
	if len(got) != 1 || got[0].Kind != BlockQuote || got[0].Ru != "" || !got[0].Verified {
		t.Fatalf("врезка без ru = %+v", got)
	}
}

func TestAssemblerNotes(t *testing.T) {
	got := feed(t, nil, ">> вывод: середина относительно нас\n>> [факт]: Земля не неподвижна\n>> [посылка]: на чём держится довод\n")
	kinds := []string{"вывод", "факт", "посылка"}
	if len(got) != 3 {
		t.Fatalf("блоки = %+v", got)
	}
	for i, k := range kinds {
		if got[i].Kind != k {
			t.Fatalf("блок %d = %+v, ожидался %s", i, got[i], k)
		}
	}
}
