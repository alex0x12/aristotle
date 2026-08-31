// internal/render/verify_test.go
package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
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

// Раунд 3 ошибочно считал это атакой; по решению координатора (1 сентября
// 2026, после независимой проверки) ожидание изменено на противоположное.
//
// Настоящий текст главы «О душе» II.1 на этом месте — "soul is the first
// grade of actuality of a natural body" (11 слов подряд). Цитата "soul first
// actuality of a natural body" (7 слов) — подпоследовательность этого куска:
// минимальное окно ровно 11 слов (предел для 7-словной цитаты — 3*7+10=31),
// выброшены "is the" и "grade of". Это законная элизия — читатель, пошедший
// по ссылке, найдёт место сразу, — а не пересказ вразброс: слова не собраны
// по одному из разных концов главы, они лежат подряд в одном предложении.
// Дополнительно: "first actuality" и "first grade of actuality" — два
// принятых перевода одного греческого ἐντελέχεια ἡ πρώτη, смысл не искажён.
//
// Раньше тест проходил ТОЛЬКО из-за дефекта №1 (жадный якорь садился на
// случайное "soul" в начале главы, за сотни слов до настоящего места, и
// разброс 373 превышал предел). После починки минимального окна выяснилось,
// что сама атака была ложной: две другие атаки этого раунда (Politics,
// Ethics, ниже) настоящие — слова там подлинно разбросаны по разным
// предложениям и минимальное окно у них подтверждённо большое.
func TestAcceptsElidedQuoteFromRealChapterSoul(t *testing.T) {
	chapter := loadCorpusChapter(t, "on-the-soul/ii/01.md")
	q := "soul first actuality of a natural body"
	if !Verify(q, []string{chapter}) {
		t.Fatal("законная элизия из настоящей главы должна проходить")
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

// --- VerifyInChapter: сверка обязана искать выдержку именно в главе с той
// ссылкой, которую заявила модель, а не в любой из пришедших. ---

func TestVerifyInChapterAcceptsRightCiteRightText(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "Nicomachean Ethics I.7", Text: passage},
		{Cite: "Politics I.2", Text: "man is naturally a political animal"},
	}
	q := "an activity of soul in accordance with virtue"
	if !VerifyInChapter(q, "Nicomachean Ethics I.7", chapters) {
		t.Fatal("правильная ссылка с правильным текстом должна подтверждаться")
	}
}

// Текст реально лежит в одной из пришедших глав, но заявлен под ссылкой на
// ДРУГУЮ главу того же пула. Это ровно та подделка, ради предотвращения
// которой ссылка вообще проверяется.
func TestVerifyInChapterRejectsTextUnderWrongCite(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "Nicomachean Ethics I.7", Text: passage},
		{Cite: "Politics I.2", Text: "man is naturally a political animal"},
	}
	q := "an activity of soul in accordance with virtue"
	if VerifyInChapter(q, "Politics I.2", chapters) {
		t.Fatal("правильный текст под чужой ссылкой из того же пула не должен подтверждаться")
	}
}

// Ссылка, которой не было в выдаче вовсе.
func TestVerifyInChapterRejectsUnknownCite(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "Nicomachean Ethics I.7", Text: passage},
	}
	q := "an activity of soul in accordance with virtue"
	if VerifyInChapter(q, "Metaphysics IV.3", chapters) {
		t.Fatal("ссылка, которой не было в выдаче, не должна подтверждаться")
	}
}

// Сравнение ссылок — строгое равенство после TrimSpace: пробелы по краям не
// должны портить сверку.
func TestVerifyInChapterTrimsCiteWhitespace(t *testing.T) {
	chapters := []corpus.Chapter{
		{Cite: "  Nicomachean Ethics I.7  ", Text: passage},
	}
	q := "an activity of soul in accordance with virtue"
	if !VerifyInChapter(q, "Nicomachean Ethics I.7", chapters) {
		t.Fatal("пробелы по краям ссылки не должны мешать сверке")
	}
}

// elidedSamples нарезает главу на окна по 14-20 нормализованных слов, из
// середины каждого окна выбрасывает два слова — так модель обрывает цитату
// многоточием или склеивает соседние предложения, — и возвращает список
// получившихся выдержек вместе с текстом главы, из которой они нарезаны.
func elidedSamples(t *testing.T, chapterText string) []string {
	t.Helper()
	words := strings.Fields(Normalize(chapterText))
	var samples []string
	for length := 14; length <= 20; length += 2 {
		for start := 0; start+length <= len(words); start += 5 {
			span := words[start : start+length]
			mid := length / 2
			elided := make([]string, 0, length-2)
			elided = append(elided, span[:mid]...)
			elided = append(elided, span[mid+2:]...)
			samples = append(samples, strings.Join(elided, " "))
		}
	}
	return samples
}

// TestAcceptsElidedExcerptsFromRealChapters — обязательный тест дефекта
// №1 итогового ревью: законные выдержки с элизией из настоящих глав корпуса
// обязаны приниматься. Раньше жадный поиск подпоследовательности цеплялся
// за первое вхождение первого слова цитаты в главе, и на реальных главах это
// бракетовало примерно две трети законных выдержек. Доля принятых обязана
// быть выше 95%.
func TestAcceptsElidedExcerptsFromRealChapters(t *testing.T) {
	chapters := []string{
		"nicomachean-ethics/i/07.md",
		"physics/ii/03.md",
		"on-the-soul/ii/01.md",
		"politics/i/02.md",
		"poetics/06.md",
	}

	var total, accepted int
	for _, rel := range chapters {
		text := loadCorpusChapter(t, rel)
		for _, q := range elidedSamples(t, text) {
			total++
			if Verify(q, []string{text}) {
				accepted++
			}
		}
	}
	if total == 0 {
		t.Fatal("не набралось ни одного образца — проверь пути до глав корпуса")
	}
	rate := float64(accepted) / float64(total)
	t.Logf("принято %d из %d законных выдержек с элизией (%.1f%%)", accepted, total, rate*100)
	if rate <= 0.95 {
		t.Fatalf("доля принятых законных выдержек с элизией = %.1f%% (%d/%d), нужно выше 95%%",
			rate*100, accepted, total)
	}
}
