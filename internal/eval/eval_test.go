package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

func TestLoadQuestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.yaml")
	body := "- ask: what is happiness\n  expect: nicomachean-ethics/i/07.md\n" +
		"- ask: как настроить nginx\n  note: должен спросить определение\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	qs, err := LoadQuestions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 2 {
		t.Fatalf("получено %d вопросов", len(qs))
	}
	if qs[0].Expect != "nicomachean-ethics/i/07.md" {
		t.Errorf("Expect = %q", qs[0].Expect)
	}
	if qs[1].Expect != "" || qs[1].Note == "" {
		t.Errorf("ловушка разобрана неверно: %+v", qs[1])
	}
}

func TestRecallAtFindsExpected(t *testing.T) {
	hits := []corpus.Hit{
		{Chapter: corpus.Chapter{Path: "politics/i/02.md"}},
		{Chapter: corpus.Chapter{Path: "nicomachean-ethics/i/07.md"}},
	}
	if !RecallAt(hits, "nicomachean-ethics/i/07.md") {
		t.Fatal("ожидаемая глава есть в выдаче, но не найдена")
	}
}

func TestRecallAtMisses(t *testing.T) {
	hits := []corpus.Hit{{Chapter: corpus.Chapter{Path: "physics/ii/03.md"}}}
	if RecallAt(hits, "poetics/06.md") {
		t.Fatal("ложное срабатывание")
	}
}

func TestRecallAtEmptyExpectIsNotCounted(t *testing.T) {
	if RecallAt(nil, "") {
		t.Fatal("ловушка без адреса не участвует в recall")
	}
}
