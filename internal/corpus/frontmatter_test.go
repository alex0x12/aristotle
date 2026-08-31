package corpus

import (
	"strings"
	"testing"
)

const chapterFile = `---
work: "Nicomachean Ethics"
work_slug: nicomachean-ethics
book: "I"
chapter: 7
group: ethics
authenticity: genuine
translator: "W. D. Ross"
source: mit
cite: "Nicomachean Ethics I.7"
---

The good of man is an activity of soul.

Second paragraph.
`

func TestParseChapter(t *testing.T) {
	c, err := ParseChapter("nicomachean-ethics/i/07.md", []byte(chapterFile))
	if err != nil {
		t.Fatal(err)
	}
	if c.Work != "Nicomachean Ethics" {
		t.Errorf("Work = %q", c.Work)
	}
	if c.Book != "I" {
		t.Errorf("Book = %q", c.Book)
	}
	if c.Number != 7 {
		t.Errorf("Number = %d", c.Number)
	}
	if c.Authenticity != "genuine" {
		t.Errorf("Authenticity = %q", c.Authenticity)
	}
	if c.Cite != "Nicomachean Ethics I.7" {
		t.Errorf("Cite = %q", c.Cite)
	}
	if c.Path != "nicomachean-ethics/i/07.md" {
		t.Errorf("Path = %q", c.Path)
	}
	want := "The good of man is an activity of soul.\n\nSecond paragraph."
	if c.Text != want {
		t.Errorf("Text = %q", c.Text)
	}
}

func TestParseChapterNullBook(t *testing.T) {
	src := "---\nwork: \"Poetics\"\nwork_slug: poetics\nbook: null\n" +
		"chapter: 6\ngroup: rhetoric-poetics\nauthenticity: genuine\n" +
		"translator: null\nsource: mit\ncite: \"Poetics 6\"\n---\n\ntext\n"
	c, err := ParseChapter("poetics/06.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if c.Book != "" {
		t.Errorf("Book = %q, ожидалась пустая строка", c.Book)
	}
	if c.Translator != "" {
		t.Errorf("Translator = %q", c.Translator)
	}
}

func TestParseChapterRejectsMissingFrontmatter(t *testing.T) {
	if _, err := ParseChapter("x.md", []byte("просто текст")); err == nil {
		t.Fatal("ожидалась ошибка при отсутствии frontmatter")
	}
}

const unclosedChapter = `---
work: "W"
work_slug: w
chapter: 1
group: g
authenticity: genuine
source: mit
cite: "C"

тело без закрывающего разделителя
`

func TestParseChapterUnclosedFrontmatterFails(t *testing.T) {
	if _, err := ParseChapter("x.md", []byte(unclosedChapter)); err == nil {
		t.Fatal("ожидалась ошибка при незакрытом frontmatter")
	}
}

const zeroChapterNumber = `---
work: "W"
work_slug: w
chapter: 0
group: g
authenticity: genuine
source: mit
cite: "C"
---

тело
`

func TestParseChapterZeroChapterNumberFails(t *testing.T) {
	if _, err := ParseChapter("x.md", []byte(zeroChapterNumber)); err == nil {
		t.Fatal("ожидалась ошибка при chapter <= 0")
	}
}

// TestParseChapterMissingRequiredField проверяет каждое обязательное поле по
// отдельности: без него ParseChapter обязана вернуть ошибку, называющую и
// файл, и само поле. book и translator сюда не входят — они законно бывают
// null.
func TestParseChapterMissingRequiredField(t *testing.T) {
	lines := map[string]string{
		"work":         `work: "W"`,
		"work_slug":    "work_slug: w",
		"chapter":      "chapter: 1",
		"group":        "group: g",
		"authenticity": "authenticity: genuine",
		"source":       "source: mit",
		"cite":         `cite: "C"`,
	}
	order := []string{"work", "work_slug", "chapter", "group", "authenticity", "source", "cite"}

	for _, missing := range order {
		t.Run(missing, func(t *testing.T) {
			var kept []string
			for _, k := range order {
				if k != missing {
					kept = append(kept, lines[k])
				}
			}
			src := "---\n" + strings.Join(kept, "\n") + "\n---\n\nтело\n"

			_, err := ParseChapter("x.md", []byte(src))
			if err == nil {
				t.Fatalf("ожидалась ошибка при отсутствии поля %s", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("ошибка не называет недостающее поле %q: %v", missing, err)
			}
			if !strings.Contains(err.Error(), "x.md") {
				t.Errorf("ошибка не называет файл: %v", err)
			}
		})
	}
}

const colonInValueChapter = `---
work: "W"
work_slug: w
chapter: 1
group: g
authenticity: genuine
source: mit
cite: "Nicomachean Ethics I.7: о благе"
---

тело
`

func TestParseChapterColonInsideValue(t *testing.T) {
	c, err := ParseChapter("x.md", []byte(colonInValueChapter))
	if err != nil {
		t.Fatal(err)
	}
	want := "Nicomachean Ethics I.7: о благе"
	if c.Cite != want {
		t.Errorf("Cite = %q, хотели %q", c.Cite, want)
	}
}

const escapedQuoteChapter = `---
work: "W"
work_slug: w
chapter: 1
group: g
authenticity: genuine
source: mit
cite: "Book \"Special\" I.7"
---

тело
`

func TestParseChapterEscapedQuoteInValue(t *testing.T) {
	c, err := ParseChapter("x.md", []byte(escapedQuoteChapter))
	if err != nil {
		t.Fatal(err)
	}
	want := `Book "Special" I.7`
	if c.Cite != want {
		t.Errorf("Cite = %q, хотели %q", c.Cite, want)
	}
}
