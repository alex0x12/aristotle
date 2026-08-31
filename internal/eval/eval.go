// internal/eval/eval.go
package eval

import (
	"bufio"
	"os"
	"strings"

	"github.com/alex0x12/aristotle/internal/corpus"
)

type Question struct {
	Ask    string
	Expect string
	Group  string
	Note   string
}

type Report struct {
	Total      int // вопросов с ожидаемым адресом
	Found      int
	Recall     float64
	Quotes     int // всего врезок в ответах
	Fabricated int // врезок, не найденных в полученных главах
}

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// LoadQuestions читает плоский YAML-список. Формат наш собственный,
// поэтому библиотека не нужна.
func LoadQuestions(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Question
	var cur *Question
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, Question{})
			cur = &out[len(out)-1]
			trimmed = strings.TrimSpace(trimmed[2:])
		}
		if cur == nil {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = unquote(value)
		switch strings.TrimSpace(key) {
		case "ask":
			cur.Ask = value
		case "expect":
			cur.Expect = value
		case "group":
			cur.Group = value
		case "note":
			cur.Note = value
		}
	}
	return out, sc.Err()
}

// RecallAt отвечает, попала ли ожидаемая глава в выдачу поиска.
func RecallAt(hits []corpus.Hit, expect string) bool {
	if expect == "" {
		return false
	}
	for _, h := range hits {
		if h.Chapter.Path == expect {
			return true
		}
	}
	return false
}

func (r *Report) Finish() {
	if r.Total > 0 {
		r.Recall = float64(r.Found) / float64(r.Total)
	}
}
