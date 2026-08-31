package corpus

import "testing"

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
