// internal/render/ansi_test.go
package render

import (
	"strings"
	"testing"
)

func printed(evs []Event, chapters []string) (string, *Printer) {
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
	chapters := []string{"the good of man is an activity of soul in accordance with virtue"}
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
	chapters := []string{"the good of man is an activity of soul"}
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
	chapters := []string{"an activity of soul"}
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
	chapters := []string{"текст"}
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
	_, p := printed([]Event{{Kind: En, Text: "x"}}, []string{"x"})
	p.Reset()
	if len(p.Sources()) != 0 {
		t.Fatal("Reset должен очищать накопленные оригиналы")
	}
}
