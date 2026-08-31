// internal/render/markers_test.go
package render

import "testing"

func collect(p *Parser, deltas ...string) []Event {
	var out []Event
	for _, d := range deltas {
		out = append(out, p.Write(d)...)
	}
	return append(out, p.Flush()...)
}

func TestProseLine(t *testing.T) {
	got := collect(&Parser{}, "Начнём с явлений.\n")
	if len(got) != 1 || got[0].Kind != Prose {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != "Начнём с явлений." {
		t.Fatalf("Text = %q", got[0].Text)
	}
}

func TestQuoteTriplet(t *testing.T) {
	got := collect(&Parser{},
		">> cite: Nicomachean Ethics I.7\n",
		">> en: the good of man is an activity of soul\n",
		">> ru: благо человека есть деятельность души\n")
	if len(got) != 3 {
		t.Fatalf("получено %d событий: %+v", len(got), got)
	}
	if got[0].Kind != Cite || got[0].Text != "Nicomachean Ethics I.7" {
		t.Errorf("cite: %+v", got[0])
	}
	if got[1].Kind != En || got[1].Text != "the good of man is an activity of soul" {
		t.Errorf("en: %+v", got[1])
	}
	if got[2].Kind != Ru {
		t.Errorf("ru: %+v", got[2])
	}
}

func TestRussianMarkers(t *testing.T) {
	got := collect(&Parser{},
		">> вывод: этого я не писал\n",
		">> [факт]: самозарождения нет\n",
		">> [посылка]: держится на природном различии\n")
	want := []Kind{Inference, FactNote, PremiseNote}
	for i, k := range want {
		if got[i].Kind != k {
			t.Errorf("событие %d: Kind = %v, ожидался %v", i, got[i].Kind, k)
		}
	}
	if got[0].Text != "этого я не писал" {
		t.Errorf("Text = %q", got[0].Text)
	}
}

func TestSplitAcrossDeltas(t *testing.T) {
	got := collect(&Parser{}, ">> ci", "te: Nicomache", "an Ethics I.7\n")
	if len(got) != 1 || got[0].Kind != Cite {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != "Nicomachean Ethics I.7" {
		t.Fatalf("Text = %q", got[0].Text)
	}
}

func TestUnknownMarkerIsProse(t *testing.T) {
	got := collect(&Parser{}, ">> цитата: опечатка модели\n")
	if len(got) != 1 || got[0].Kind != Prose {
		t.Fatalf("%+v", got)
	}
	if got[0].Text != ">> цитата: опечатка модели" {
		t.Fatalf("строка должна пройти как есть, получено %q", got[0].Text)
	}
}

func TestFlushEmitsTailLine(t *testing.T) {
	p := &Parser{}
	p.Write("хвост без перевода строки")
	got := p.Flush()
	if len(got) != 1 || got[0].Text != "хвост без перевода строки" {
		t.Fatalf("%+v", got)
	}
}

func TestFlushTwiceEmitsNothing(t *testing.T) {
	p := &Parser{}
	p.Write("хвост")
	p.Flush()
	if got := p.Flush(); len(got) != 0 {
		t.Fatalf("повторный Flush выдал %+v", got)
	}
}

func TestBlankLinePreserved(t *testing.T) {
	got := collect(&Parser{}, "первый\n", "\n", "второй\n")
	if len(got) != 3 {
		t.Fatalf("пустая строка должна сохраниться: %+v", got)
	}
	if got[1].Text != "" || got[1].Kind != Prose {
		t.Fatalf("%+v", got[1])
	}
}
