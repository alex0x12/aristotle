package corpus

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Chapter — одна глава корпуса вместе с её метаданными.
type Chapter struct {
	Work         string
	WorkSlug     string
	Book         string
	Number       int
	Group        string
	Authenticity string
	Translator   string
	Source       string
	Cite         string
	Text         string
	Path         string
}

const delim = "---"

func unquote(v string) string {
	v = strings.TrimSpace(v)
	if v == "null" || v == "" {
		return ""
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	return v
}

// ParseChapter разбирает файл главы. Frontmatter плоский — его пишет
// build/emit.py, — поэтому YAML-библиотека не нужна.
func ParseChapter(path string, data []byte) (Chapter, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	if !sc.Scan() || strings.TrimSpace(sc.Text()) != delim {
		return Chapter{}, fmt.Errorf("%s: нет frontmatter", path)
	}

	c := Chapter{Path: path}
	closed := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == delim {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = unquote(value)
		switch strings.TrimSpace(key) {
		case "work":
			c.Work = value
		case "work_slug":
			c.WorkSlug = value
		case "book":
			c.Book = value
		case "chapter":
			n, err := strconv.Atoi(value)
			if err != nil {
				return Chapter{}, fmt.Errorf("%s: chapter %q: %w", path, value, err)
			}
			c.Number = n
		case "group":
			c.Group = value
		case "authenticity":
			c.Authenticity = value
		case "translator":
			c.Translator = value
		case "source":
			c.Source = value
		case "cite":
			c.Cite = value
		}
	}
	if !closed {
		return Chapter{}, fmt.Errorf("%s: frontmatter не закрыт", path)
	}

	var body strings.Builder
	for sc.Scan() {
		body.WriteString(sc.Text())
		body.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return Chapter{}, fmt.Errorf("%s: %w", path, err)
	}
	c.Text = strings.TrimSpace(body.String())

	// book и translator законно бывают null (работа без книг, переводчик не
	// указан в шапке) — обязательными не считаются. Остальные поля обязаны
	// быть заполнены, иначе, например, глава без authenticity молча пройдёт
	// фильтр spurious как подлинная.
	for _, req := range []struct{ name, value string }{
		{"work", c.Work},
		{"work_slug", c.WorkSlug},
		{"group", c.Group},
		{"authenticity", c.Authenticity},
		{"source", c.Source},
		{"cite", c.Cite},
	} {
		if req.value == "" {
			return Chapter{}, fmt.Errorf("%s: отсутствует обязательное поле %s", path, req.name)
		}
	}
	if c.Number <= 0 {
		return Chapter{}, fmt.Errorf("%s: отсутствует обязательное поле chapter", path)
	}
	return c, nil
}
