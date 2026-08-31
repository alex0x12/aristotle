// internal/render/verify_test.go
package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadCorpusChapter читает главу корпуса и снимает YAML-frontmatter между
// строками "---", возвращая только текст главы.
func loadCorpusChapter(t *testing.T, relPath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "corpus", relPath))
	if err != nil {
		t.Fatalf("не удалось прочитать главу корпуса %s: %v", relPath, err)
	}
	parts := strings.SplitN(string(raw), "---", 3)
	if len(parts) != 3 {
		t.Fatalf("не удалось снять frontmatter главы %s", relPath)
	}
	return parts[2]
}

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
// Нечёткий шаг: цитата из двух разных частей главы, пропускающая среднее предложение.
func TestAcceptsJoinedSentences(t *testing.T) {
	chapter := "the good of man turns out to be. an activity of soul in accordance with virtue. and if there are more than one virtue."
	// Цитата склеивает первое и третье предложения, пропуская второе.
	q := "the good of man turns out to be and if there are more than one virtue"
	if !Verify(q, []string{chapter}) {
		t.Fatal("склейка предложений пропусками должна проходить как подпоследовательность")
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

// Раунд 2: цитата с подмешанным выдуманным словом отвергается.
// Это была атака ревьюера: двенадцать настоящих слов плюс одно выдуманное даёт 92.3%.
func TestRejectsInjectedWord(t *testing.T) {
	q := "the good of man turns out to be an activity of soul unicorns"
	if Verify(q, []string{passage}) {
		t.Fatal("цитата с подмешанным выдуманным словом не должна проходить")
	}
}

// Раунд 3: пересказ, а не цитата. "By" из "by nature" встречается в главе
// далеко от "man is ... a political animal" ("is by nature the superior и
// governor" в начале главы), и жадный поиск подпоследовательности растягивает
// совпадение почти на всю главу. Настоящий текст главы — "man is naturally a
// political animal", без "by nature". Без ограничения на разброс это
// засчитывалось как подтверждённая цитата.
func TestRejectsParaphraseSpreadAcrossChapterPolitics(t *testing.T) {
	chapter := loadCorpusChapter(t, "politics/i/02.md")
	q := "man is by nature a political animal"
	if Verify(q, []string{chapter}) {
		t.Fatal("пересказ, размазанный по главе, не должен проходить как цитата")
	}
}

// Раунд 3: тот же пробой на главе «О душе» II.1. Настоящий текст —
// "the first grade of actuality of a natural body", а не "soul first
// actuality of a natural body"; слово "soul" встречается многократно по всей
// главе, и жадный поиск подхватывает первое попавшееся.
func TestRejectsParaphraseSpreadAcrossChapterSoul(t *testing.T) {
	chapter := loadCorpusChapter(t, "on-the-soul/ii/01.md")
	q := "soul first actuality of a natural body"
	if Verify(q, []string{chapter}) {
		t.Fatal("пересказ, размазанный по главе, не должен проходить как цитата")
	}
}

// Раунд 3: тот же пробой на «Никомаховой этике» I.7. Слова "happiness",
// "function of man" и "activity of soul" реально есть в главе, но в разных
// предложениях далеко друг от друга — это пересказ, собранный из частых слов
// главы, а не цитата.
func TestRejectsParaphraseSpreadAcrossChapterEthics(t *testing.T) {
	chapter := loadCorpusChapter(t, "nicomachean-ethics/i/07.md")
	q := "happiness function of man activity of soul"
	if Verify(q, []string{chapter}) {
		t.Fatal("пересказ, размазанный по главе, не должен проходить как цитата")
	}
}

// Законный случай: дословная выдержка из настоящей главы «Никомаховой этики»
// I.7 должна проходить — узкое совпадение, а не разброс пересказа.
func TestAcceptsVerbatimQuoteFromRealChapter(t *testing.T) {
	chapter := loadCorpusChapter(t, "nicomachean-ethics/i/07.md")
	q := "human good turns out to be activity of soul in accordance with virtue"
	if !Verify(q, []string{chapter}) {
		t.Fatal("дословная выдержка из настоящей главы должна проходить")
	}
}
