package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alex0x12/aristotle/internal/chat"
)

func TestExportMarkdown(t *testing.T) {
	md := exportMarkdown("О добродетели", []chat.Record{
		{Role: "user", Text: "Что такое добродетель?"},
		{Role: "assistant", Text: "Надвое.\n>> cite: NE II.1\n>> en: moral virtue\n>> ru: нравственная добродетель\n" +
			">> cite: NE II.6\n>> en: invented\n>> ru: выдумка\n>> вывод: середина относительно нас\n",
			Quotes: []chat.Quote{{Cite: "NE II.1", Verified: true}, {Cite: "NE II.6", Verified: false}}},
	})
	for _, want := range []string{"# О добродетели", "**Вопрос.** Что такое добродетель?", "Надвое.",
		"> нравственная добродетель\n>\n> — NE II.1\n", "> — NE II.6 *(нет в выдержках)*", "*moral virtue*",
		"**[вывод]** середина относительно нас"} {
		if !strings.Contains(md, want) {
			t.Errorf("нет %q в\n%s", want, md)
		}
	}
}

// Под врезкой — русское название работы, а английская ссылка остаётся рядом
// (спека, «Changes» №1); работа, которой нет в таблице, остаётся английской.
func TestExportShowsRussianCiteWithEnglishNextToIt(t *testing.T) {
	md := exportMarkdown("Беседа", []chat.Record{
		{Role: "user", Text: "Что такое добродетель?"},
		{Role: "assistant", Text: ">> cite: Nicomachean Ethics II.1\n>> en: moral virtue\n>> ru: нравственная добродетель\n" +
			">> cite: On the Soul 3\n>> en: soul\n>> ru: душа\n" +
			">> cite: Unknown Treatise 7\n>> en: x\n>> ru: икс\n",
			Quotes: []chat.Quote{{Cite: "Nicomachean Ethics II.1", Verified: true}, {Cite: "On the Soul 3", Verified: true},
				{Cite: "Unknown Treatise 7", Verified: true}}},
	})
	for _, want := range []string{
		"> нравственная добродетель\n>\n> — Никомахова этика II.1 (Nicomachean Ethics II.1)\n",
		"> — О душе 3 (On the Soul 3)\n",
		"> икс\n>\n> — Unknown Treatise 7\n", // неизвестная работа — как есть, без скобок
	} {
		if !strings.Contains(md, want) {
			t.Errorf("нет %q в\n%s", want, md)
		}
	}
	if strings.Contains(md, "Unknown Treatise 7 (") {
		t.Errorf("английская ссылка продублирована в скобках:\n%s", md)
	}
}

// Остановленный и сверка-неизвестна — помечены; заголовок в одну строку.
func TestExportMarksAndKeepsTitleOnOneLine(t *testing.T) {
	md := exportMarkdown("Первая\nстрока\r\n# вторая", []chat.Record{
		{Role: "user", Text: "вопрос"},
		{Role: "assistant", Text: "начало\n>> cite: Politics 1\n>> en: a\n>> ru: б\n", StopReason: "cancelled"},
	})
	if !strings.HasPrefix(md, "# Первая строка # вторая\n\n") {
		t.Errorf("заголовок не в одну строку:\n%s", md)
	}
	for _, want := range []string{"*(сверка не сохранена)*", "*(ответ остановлен)*", "> — Политика 1 (Politics 1) *(сверка не сохранена)*"} {
		if !strings.Contains(md, want) {
			t.Errorf("нет %q в\n%s", want, md)
		}
	}
}

func TestExportEndpointHeaders(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeJournal(t, s.Paths.History, convID,
		chat.Record{Role: "user", Text: "Что такое добродетель?"},
		chat.Record{Role: chat.RoleMeta, Title: "О добродетели"},
		chat.Record{Role: "assistant", Text: "Надвое.\n"})
	rec := do(t, s.Handler(), http.MethodGet, "/api/conversations/"+convID+"/export.md", "")
	if rec.Code != 200 {
		t.Fatalf("код %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `filename="aristotle-`+convID+`.md"`) {
		t.Errorf("ASCII-запасное имя: %q", cd)
	}
	name := dispositionName(t, cd)
	if name != "О добродетели.md" {
		t.Errorf("filename* = %q", name)
	}
	if !strings.Contains(rec.Body.String(), "# О добродетели") {
		t.Errorf("тело: %s", rec.Body.String())
	}
	// Новая беседа без единого вопроса: ссылка «Экспорт» на экране уже есть, и
	// ответ об ошибке увёл бы со страницы. Выгрузка пустая, но настоящая.
	empty := do(t, s.Handler(), http.MethodGet, "/api/conversations/2026-01-01T00-00-00/export.md", "")
	if empty.Code != 200 || empty.Body.String() != "# Новая беседа\n\n" {
		t.Errorf("пустая беседа: код %d, %q", empty.Code, empty.Body.String())
	}
}

var filenameStarRe = regexp.MustCompile(`filename\*=UTF-8''([^;]*)`)

// dispositionName достаёт и раскодирует filename* из заголовка.
func dispositionName(t *testing.T, cd string) string {
	t.Helper()
	m := filenameStarRe.FindStringSubmatch(cd)
	if m == nil {
		t.Fatalf("в заголовке нет filename*: %q", cd)
	}
	name, err := url.PathUnescape(m[1])
	if err != nil {
		t.Fatalf("filename* %q не раскодировать: %v", m[1], err)
	}
	return name
}

// Название беседы приходит от пользователя (и из правленого вручную журнала):
// в заголовок ответа оно не должно пронести ни перевод строки, ни кавычку, ни
// путь, ни бесконечную длину.
func TestExportDispositionIsSafeForHostileTitle(t *testing.T) {
	hostile := []string{
		"\"\r\nX-Evil: 1",
		`кавычки " и ' и \ и / и ../../etc/passwd`,
		"a\x00b\x1f\x7fc",
		strings.Repeat("Очень длинное название ", 100),
		"   ...   ",
		"; filename=\"evil.sh\"",
		"%0d%0aX-Evil: 1",
	}
	for i, title := range hostile {
		s := newTestServer(t, nil, nil)
		// журнал пишется как есть: rename через API порезал бы название, а
		// правка файла вручную — нет
		writeJournal(t, s.Paths.History, convID,
			chat.Record{Role: "user", Text: "вопрос"}, chat.Record{Role: chat.RoleMeta, Title: title})
		rec := do(t, s.Handler(), http.MethodGet, "/api/conversations/"+convID+"/export.md", "")
		if rec.Code != 200 {
			t.Fatalf("%d: код %d", i, rec.Code)
		}
		if rec.Header().Get("X-Evil") != "" {
			t.Errorf("%d: внедрён заголовок X-Evil", i)
		}
		cd := rec.Header().Get("Content-Disposition")
		if strings.ContainsAny(cd, "\r\n\x00") || strings.Count(cd, "filename=") != 1 {
			t.Errorf("%d: заголовок %q", i, cd)
		}
		if !strings.HasPrefix(cd, `attachment; filename="aristotle-`+convID+`.md"; filename*=UTF-8''`) {
			t.Errorf("%d: заголовок %q", i, cd)
		}
		name := dispositionName(t, cd)
		if strings.ContainsAny(name, "\"\\/\r\n\x00:;") || strings.Contains(name, "..") || !utf8.ValidString(name) ||
			strings.HasPrefix(name, ".") || strings.HasPrefix(name, " ") || !strings.HasSuffix(name, ".md") {
			t.Errorf("%d: имя файла %q", i, name)
		}
		if n := utf8.RuneCountInString(name); n > 83 { // 80 знаков названия + «.md»
			t.Errorf("%d: имя файла из %d знаков", i, n)
		}
		// закодированная часть — только знаки RFC 8187 attr-char и %XX
		enc := filenameStarRe.FindStringSubmatch(cd)[1]
		if !regexp.MustCompile("^[A-Za-z0-9!#$&+.^_`|~%-]+$").MatchString(enc) {
			t.Errorf("%d: filename* %q содержит лишние знаки", i, enc)
		}
		if len(cd) > 600 {
			t.Errorf("%d: заголовок в %d байт", i, len(cd))
		}
	}
}
