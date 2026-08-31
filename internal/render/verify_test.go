// internal/render/verify_test.go
package render

import "testing"

const passage = "But the good of man turns out to be an activity of soul " +
	"in accordance with virtue, and if there are more than one virtue, " +
	"in accordance with the best and most complete."

func TestNormalizeStripsCaseAndPunctuation(t *testing.T) {
	if got := Normalize("The Good,  of Man!"); got != "the good of man" {
		t.Fatalf("Normalize = %q", got)
	}
}

func TestVerifyExactSubstring(t *testing.T) {
	q := "an activity of soul in accordance with virtue"
	if !Verify(q, []string{passage}) {
		t.Fatal("точная подстрока должна находиться")
	}
}

func TestVerifyIgnoresCaseAndPunctuation(t *testing.T) {
	q := "An Activity of Soul, in accordance with Virtue!"
	if !Verify(q, []string{passage}) {
		t.Fatal("нормализация должна снимать регистр и пунктуацию")
	}
}

func TestVerifyAcceptsElidedQuote(t *testing.T) {
	q := "the good of man turns out to be an activity of soul ... " +
		"in accordance with the best and most complete"
	if !Verify(q, []string{passage}) {
		t.Fatal("оборванная многоточием цитата должна проходить по словам")
	}
}

func TestVerifyRejectsFabrication(t *testing.T) {
	q := "happiness is the reward of a life devoted to contemplation of the stars"
	if Verify(q, []string{passage}) {
		t.Fatal("выдуманная цитата не должна проходить")
	}
}

func TestVerifyAcrossSeveralChapters(t *testing.T) {
	q := "an activity of soul in accordance with virtue"
	if !Verify(q, []string{"нерелевантная глава", passage}) {
		t.Fatal("искать надо во всех выдержках")
	}
}

func TestVerifyRejectsWordsSpreadAcrossChapters(t *testing.T) {
	a := "the good of man turns out to be"
	b := "an activity of soul in accordance with virtue"
	q := "the good of man turns out to be an activity of soul in accordance with virtue"
	if Verify(q, []string{a, b}) {
		t.Fatal("слова из разных глав не образуют цитату")
	}
}

func TestVerifyEmptyQuoteIsFalse(t *testing.T) {
	if Verify("   ", []string{passage}) {
		t.Fatal("пустая выдержка не подтверждается")
	}
}

func TestVerifyNoChaptersIsFalse(t *testing.T) {
	if Verify("an activity of soul", nil) {
		t.Fatal("без выдержек подтверждать нечем")
	}
}
