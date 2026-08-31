package render

import (
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

func printed(evs []Event, chapters []corpus.Chapter) (string, *Printer) {
	var b strings.Builder
	p := &Printer{Out: &b, Color: false}
	for _, e := range evs {
		p.Handle(e, chapters)
	}
	return b.String(), p
}

func TestProsePrintedAsIs(t *testing.T) {
	out, _ := printed([]Event{{Kind: Prose, Text: "Начнём с явлений."}}, nil)
	if out != "Начнём с явлений.\n" {
		t.Fatalf("%q", out)
	}
}

func TestVerifiedQuoteHasNoWarning(t *testing.T) {
	chapters := []corpus.Chapter{{
		Cite: "Nicomachean Ethics I.7",
		Text: "the good of man is an activity of soul in accordance with virtue",
	}}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Nicomachean Ethics I.7"},
		{Kind: En, Text: "an activity of soul in accordance with virtue"},
		{Kind: Ru, Text: "деятельность души сообразно добродетели"},
	}, chapters)
	if strings.Contains(out, "[!]") {
		t.Fatalf("подтверждённая выдержка помечена как выдуманная:\n%s", out)
	}
	if !strings.Contains(out, "Nicomachean Ethics I.7") {
		t.Fatalf("нет ссылки:\n%s", out)
	}
	if !strings.Contains(out, "деятельность души") {
		t.Fatalf("нет русского текста:\n%s", out)
	}
}

func TestFabricatedQuoteIsFlagged(t *testing.T) {
	chapters := []corpus.Chapter{{
		Cite: "Nicomachean Ethics I.7",
		Text: "the good of man is an activity of soul",
	}}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Nicomachean Ethics I.7"},
		{Kind: En, Text: "happiness is the reward of contemplating the stars"},
		{Kind: Ru, Text: "счастье есть награда за созерцание звёзд"},
	}, chapters)
	if !strings.Contains(out, "[!] нет в выдержках") {
		t.Fatalf("выдуманная выдержка не помечена:\n%s", out)
	}
}

func TestEnglishLineIsNotPrintedButCollected(t *testing.T) {
	chapters := []corpus.Chapter{{Cite: "NE I.7", Text: "an activity of soul"}}
	out, p := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "an activity of soul"},
		{Kind: Ru, Text: "деятельность души"},
	}, chapters)
	if strings.Contains(out, "an activity of soul") {
		t.Fatal("английский оригинал не печатается сразу, он доступен по /src")
	}
	src := p.Sources()
	if len(src) != 1 || !strings.Contains(src[0], "an activity of soul") {
		t.Fatalf("Sources = %+v", src)
	}
}

func TestNoteKindsAreLabelled(t *testing.T) {
	out, _ := printed([]Event{
		{Kind: Inference, Text: "этого я не писал"},
		{Kind: FactNote, Text: "самозарождения нет"},
		{Kind: PremiseNote, Text: "держится на природном различии"},
	}, nil)
	for _, want := range []string{"вывод", "факт", "посылка"} {
		if !strings.Contains(out, want) {
			t.Errorf("нет пометки %q:\n%s", want, out)
		}
	}
}

func TestOutputHasNoEmoji(t *testing.T) {
	chapters := []corpus.Chapter{{Cite: "NE I.7", Text: "текст"}}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "выдумка"},
		{Kind: Ru, Text: "выдумка"},
		{Kind: Inference, Text: "вывод"},
	}, chapters)
	for _, r := range out {
		if r >= 0x1F300 && r <= 0x1FAFF {
			t.Fatalf("в выводе эмодзи %q: на машине нет эмодзи-шрифта", r)
		}
	}
}

func TestResetClearsSources(t *testing.T) {
	_, p := printed([]Event{{Kind: En, Text: "x"}}, []corpus.Chapter{{Cite: "x", Text: "x"}})
	p.Reset()
	if len(p.Sources()) != 0 {
		t.Fatal("Reset должен очищать накопленные оригиналы")
	}
}

// TestQuoteUnderWrongCiteIsFlagged закрывает дефект итогового ревью: текст
// реально лежит в одной из пришедших глав, но модель подписала его ссылкой
// на ДРУГУЮ главу из того же пула. Раньше renderHits выбрасывал связь
// текста с главой (собирал только c.Text), и Verify спрашивал лишь "есть ли
// эта строка хоть в какой-нибудь из глав" — такая подмена проходила молча.
func TestQuoteUnderWrongCiteIsFlagged(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "Nicomachean Ethics I.7", Text: "an activity of soul in accordance with virtue"},
		{Cite: "Politics I.2", Text: "man is by nature a political animal"},
	}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Politics I.2"}, // ссылка чужая: текст на самом деле из НЭ I.7
		{Kind: En, Text: "an activity of soul in accordance with virtue"},
		{Kind: Ru, Text: "деятельность души сообразно добродетели"},
	}, chapters)
	if !strings.Contains(out, "[!] нет в выдержках") {
		t.Fatalf("текст под чужой ссылкой должен быть помечен как неподтверждённый:\n%s", out)
	}
}

// TestUnknownCiteIsFlagged: модель сослалась на главу, которой в выдаче не
// было вовсе. Тоже не подтверждено.
func TestUnknownCiteIsFlagged(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "Nicomachean Ethics I.7", Text: "an activity of soul in accordance with virtue"},
	}
	out, _ := printed([]Event{
		{Kind: Cite, Text: "Metaphysics IV.3"}, // такой главы в выдаче нет
		{Kind: En, Text: "an activity of soul in accordance with virtue"},
		{Kind: Ru, Text: "деятельность души сообразно добродетели"},
	}, chapters)
	if !strings.Contains(out, "[!] нет в выдержках") {
		t.Fatalf("ссылка, которой не было в выдаче, не должна подтверждаться:\n%s", out)
	}
}

// TestSourcesMarksUnverifiedExcerpt закрывает вторую половину дефекта: /src
// показывал непроверенные выдержки как чистые. Строка в Sources() обязана
// нести ту же пометку [!], что и врезка.
func TestSourcesMarksUnverifiedExcerpt(t *testing.T) {
	chapters := []corpus.Chapter{{Cite: "NE I.7", Text: "an activity of soul"}}
	_, p := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "happiness is contemplation of the stars"},
		{Kind: Ru, Text: "выдумка"},
	}, chapters)
	src := p.Sources()
	if len(src) != 1 || !strings.Contains(src[0], "[!]") {
		t.Fatalf("непроверенная выдержка не помечена в /src: %+v", src)
	}
}

// TestSourcesVerifiesOnEnEvenWithoutRu закрывает главный сценарий дефекта:
// модель написала cite и en, но ru не написала — обычная оплошность.
// Раньше строка копилась в sources в момент En, до всякой сверки и без
// пометки: /src показывал бы такую выдержку как чистый источник, хотя она
// вообще не проверялась. Теперь сверка делается на En, и /src обязан нести
// правильную пометку даже без ru.
func TestSourcesVerifiesOnEnEvenWithoutRu(t *testing.T) {
	chapters := []corpus.Chapter{{Cite: "NE I.7", Text: "an activity of soul"}}

	// Подтверждённая выдержка без ru: в /src не должно быть [!].
	_, verified := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "an activity of soul"},
	}, chapters)
	vsrc := verified.Sources()
	if len(vsrc) != 1 || strings.Contains(vsrc[0], "[!]") {
		t.Fatalf("подтверждённая выдержка без ru не должна помечаться в /src: %+v", vsrc)
	}

	// Выдуманная выдержка без ru: в /src обязана быть [!], а не пройти
	// чистым источником.
	_, fabricated := printed([]Event{
		{Kind: Cite, Text: "NE I.7"},
		{Kind: En, Text: "happiness is contemplation of the stars"},
	}, chapters)
	fsrc := fabricated.Sources()
	if len(fsrc) != 1 || !strings.Contains(fsrc[0], "[!]") {
		t.Fatalf("выдуманная выдержка без ru должна быть помечена в /src: %+v", fsrc)
	}
}
