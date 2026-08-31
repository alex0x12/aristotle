// internal/render/verify.go
package render

import (
	"strings"
	"unicode"

	"github.com/alex0x12/aristotle/internal/corpus"
)

// Threshold — доля слов выдержки, которую надо найти в одной главе,
// если точная подстрока не совпала. Устанавливается в 1.0 потому, что элизия
// (обрывание многоточием) выбрасывает слова главы, а не слова цитаты.
// Подлинная выдержка, пропускающая слова в исходном тексте, должна содержать
// только слова, которые действительно там есть. Допуск в 0.9 позволял бы модели
// вставлять в цитату выдуманные слова.
const Threshold = 1.0

// Normalize приводит текст к виду, в котором сравнение не зависит
// от регистра, пунктуации и переносов.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Verify отвечает, подтверждается ли выдержка хотя бы одной из глав,
// которые поиск реально вернул на этом ходу.
func Verify(quote string, chapters []string) bool {
	q := Normalize(quote)
	if q == "" || len(chapters) == 0 {
		return false
	}

	quoteWords := strings.Fields(q)
	if len(quoteWords) == 0 {
		return false
	}

	// Шаг 1: поиск точной подстроки.
	for _, c := range chapters {
		n := Normalize(c)
		if strings.Contains(n, q) {
			return true
		}
	}

	// Шаг 2: если цитата короче 6 слов, точное вхождение обязательно.
	if len(quoteWords) < 6 {
		return false
	}

	// Шаг 2: поиск минимального окна подпоследовательности с сохранением
	// порядка. Слова считаются только внутри одной главы: набор из разных
	// глав цитатой не является.
	for _, c := range chapters {
		n := Normalize(c)
		chapterWords := strings.Fields(n)

		// Прямой проход: жадно, слева направо, беря ПЕРВОЕ подходящее
		// вхождение каждого следующего слова цитаты. Этого достаточно, чтобы
		// подтвердить, что подпоследовательность вообще есть, и получить
		// lastHit — позицию последнего слова цитаты при таком выравнивании.
		// Начало окна (firstHit) этот проход не определяет: первое слово
		// цитаты может быть частым и зацепиться далеко от настоящего места.
		quoteIdx := 0
		chapterIdx := 0
		lastHit := -1

		for chapterIdx < len(chapterWords) && quoteIdx < len(quoteWords) {
			if chapterWords[chapterIdx] == quoteWords[quoteIdx] {
				lastHit = chapterIdx
				quoteIdx++
			}
			chapterIdx++
		}

		if float64(quoteIdx)/float64(len(quoteWords)) < Threshold {
			continue
		}

		// Обратный проход: от lastHit к началу, поджимая левую границу.
		// Для каждого слова цитаты, взятого с конца, берётся ПОСЛЕДНЕЕ
		// подходящее вхождение, не заходя за уже занятую позицию следующего
		// (по цитате) слова. Прямой проход уже доказал, что подпоследовательность
		// целиком укладывается в [0, lastHit], поэтому обратный проход
		// обязан найти позицию для каждого слова — окно получается
		// минимальным по построению, а не случайным по первому совпадению.
		firstHit := lastHit
		pos := lastHit
		for qi := len(quoteWords) - 1; qi >= 0; qi-- {
			for pos >= 0 && chapterWords[pos] != quoteWords[qi] {
				pos--
			}
			firstHit = pos
			pos--
		}

		spread := lastHit - firstHit + 1
		if spread <= 3*len(quoteWords)+10 {
			return true
		}
	}
	return false
}

// VerifyInChapter отвечает, подтверждается ли выдержка ровно той главой,
// чью ссылку заявила модель, — а не какой-нибудь из пришедших на этом ходу.
// Ссылки сравниваются строгим равенством после strings.TrimSpace. Если среди
// chapters нет ни одной с такой ссылкой (модель сослалась на главу, которой
// не было в выдаче, либо перепутала ссылку), выдержка не подтверждена.
func VerifyInChapter(quote, cite string, chapters []corpus.Chapter) bool {
	cite = strings.TrimSpace(cite)
	var texts []string
	for _, c := range chapters {
		if strings.TrimSpace(c.Cite) == cite {
			texts = append(texts, c.Text)
		}
	}
	return Verify(quote, texts)
}
