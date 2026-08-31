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

// Атака 1: служебные слова, собранные не по порядку.
func TestRejectsFunctionalWordsOutOfOrder(t *testing.T) {
	q := "if there are more than one and the best of man"
	if Verify(q, []string{passage}) {
		t.Fatal("служебные слова не по порядку не образуют цитату")
	}
}

// Атака 2: переставленная лексика главы.
func TestRejectsPermutedLexicon(t *testing.T) {
	q := "the best and most complete virtue turns out to be the good of man in accordance with soul"
	if Verify(q, []string{passage}) {
		t.Fatal("переставленная лексика не должна проходить")
	}
}

// Атака 3: слово повторено многократно.
func TestRejectsRepeatedWord(t *testing.T) {
	q := "the the the the the the the the the the"
	if Verify(q, []string{passage}) {
		t.Fatal("повторённое слово не должно начисляться многократно")
	}
}

// Атака 4: цитата в одно слово, отсутствующая дословно.
func TestRejectionSingleWord(t *testing.T) {
	q := "happiness"
	if Verify(q, []string{passage}) {
		t.Fatal("одно слово, отсутствующее дословно, не должна проходить")
	}
}

// Атака 4 продолжение: цитата в два слова, отсутствующая дословно.
func TestRejectionTwoWords(t *testing.T) {
	q := "happiness virtue"
	if Verify(q, []string{passage}) {
		t.Fatal("два слова, отсутствующие дословно, не должны проходить")
	}
}

// Законный случай: склейка двух соседних предложений главы.
func TestAcceptsJoinedSentences(t *testing.T) {
	q := "an activity of soul in accordance with virtue and if there are more than one virtue"
	if !Verify(q, []string{passage}) {
		t.Fatal("склейка двух соседних предложений должна проходить")
	}
}

// Законный случай: точное вхождение короткой цитаты (три слова).
func TestAcceptsExactShortQuote(t *testing.T) {
	q := "in accordance with"
	if !Verify(q, []string{passage}) {
		t.Fatal("точное вхождение короткой цитаты должна проходить")
	}
}

// Кириллица нормализуется верно.
func TestNormalizeCyrillic(t *testing.T) {
	if got := Normalize("Привет, МИР!"); got != "привет мир" {
		t.Fatalf("кириллица нормализуется неверно: %q", got)
	}
}

// Цитата длиннее главы из настоящих слов отвергается.
func TestRejectsLongerThanSource(t *testing.T) {
	chapter := "the good of man"
	q := "the good of man turns out to be an activity of soul in accordance with virtue"
	if Verify(q, []string{chapter}) {
		t.Fatal("цитата длиннее главы не должна проходить")
	}
}
