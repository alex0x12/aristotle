// internal/render/verify.go
package render

import (
	"strings"
	"unicode"
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

	// Шаг 2: жадный поиск подпоследовательности с сохранением порядка.
	// Слова считаются только внутри одной главы: набор из разных глав
	// цитатой не является.
	for _, c := range chapters {
		n := Normalize(c)
		chapterWords := strings.Fields(n)

		// Жадный поиск подпоследовательности.
		quoteIdx := 0
		chapterIdx := 0

		for chapterIdx < len(chapterWords) && quoteIdx < len(quoteWords) {
			if chapterWords[chapterIdx] == quoteWords[quoteIdx] {
				quoteIdx++
			}
			chapterIdx++
		}

		hit := quoteIdx
		if float64(hit)/float64(len(quoteWords)) >= Threshold {
			return true
		}
	}
	return false
}
