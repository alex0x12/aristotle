// internal/render/verify.go
package render

import (
	"strings"
	"unicode"
)

// Threshold — доля слов выдержки, которую надо найти в одной главе,
// если точная подстрока не совпала.
const Threshold = 0.9

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

	normalized := make([]string, 0, len(chapters))
	for _, c := range chapters {
		n := Normalize(c)
		if strings.Contains(n, q) {
			return true
		}
		normalized = append(normalized, n)
	}

	words := strings.Fields(q)
	if len(words) == 0 {
		return false
	}
	// Слова считаются только внутри одной главы: набор из разных глав
	// цитатой не является.
	for _, n := range normalized {
		present := make(map[string]bool)
		for _, w := range strings.Fields(n) {
			present[w] = true
		}
		hit := 0
		for _, w := range words {
			if present[w] {
				hit++
			}
		}
		if float64(hit)/float64(len(words)) >= Threshold {
			return true
		}
	}
	return false
}
