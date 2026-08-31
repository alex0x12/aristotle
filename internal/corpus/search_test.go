package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeChapter(t *testing.T, root, rel, authenticity, group, cite string) {
	t.Helper()
	body := "---\nwork: \"W\"\nwork_slug: w\nbook: \"I\"\nchapter: 1\n" +
		"group: " + group + "\nauthenticity: " + authenticity +
		"\ntranslator: null\nsource: mit\ncite: \"" + cite + "\"\n---\n\nтело главы\n"
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeQmd(out string) func(context.Context, ...string) ([]byte, error) {
	return func(context.Context, ...string) ([]byte, error) {
		return []byte(out), nil
	}
}

func TestSearchDropsSpurious(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "econ/i/01.md", "spurious", "ethics", "Economics I.1")

	s := &Searcher{
		Collection: "aristotle",
		Root:       root,
		Run: fakeQmd(`[
		  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
		  {"file":"qmd://aristotle/econ/i/01.md","score":0.8}
		]`),
	}
	hits, err := s.Search(context.Background(), "happiness", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("получено %d попаданий, ожидалось 1", len(hits))
	}
	if hits[0].Chapter.Cite != "NE I.1" {
		t.Fatalf("Cite = %q", hits[0].Chapter.Cite)
	}
}

func TestSearchFiltersByGroup(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "ha/i/01.md", "genuine", "biology", "HA I.1")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/ha/i/01.md","score":0.85}
	]`)}
	hits, err := s.Search(context.Background(), "х", "ethics", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chapter.Group != "ethics" {
		t.Fatalf("фильтр по group не сработал: %+v", hits)
	}
}

func TestSearchPassesCollectionAndLimit(t *testing.T) {
	var got []string
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		}}
	if _, err := s.Search(context.Background(), "happiness", "", 6); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"query", "happiness", "-c aristotle", "--json", "-n 6"} {
		if !strings.Contains(joined, strings.ReplaceAll(want, " ", " ")) {
			t.Errorf("в аргументах нет %q: %v", want, got)
		}
	}
}

func TestSearchSkipsMissingFile(t *testing.T) {
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: fakeQmd(`[{"file":"qmd://aristotle/нет/такого.md","score":0.9}]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err != nil {
		t.Fatalf("пропавший файл не должен ронять поиск: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("получено %d попаданий", len(hits))
	}
}
