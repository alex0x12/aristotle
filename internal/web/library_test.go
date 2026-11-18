package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

func chapterFile(t *testing.T, root, rel, work, slug, book string, n int, auth string) {
	t.Helper()
	b := "null"
	if book != "" {
		b = `"` + book + `"`
	}
	data := "---\nwork: \"" + work + "\"\nwork_slug: " + slug + "\nbook: " + b +
		"\nchapter: " + string(rune('0'+n)) + "\ngroup: ethics\nauthenticity: " + auth +
		"\ntranslator: \"W. D. Ross\"\nsource: mit\ncite: \"" + work + " " + strings.ToUpper(book) + "." + string(rune('0'+n)) +
		"\"\n---\n\nText of the chapter.\n"
	path := filepath.Join(root, "corpus", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// recFinder запоминает, с чем его спросили, и отдаёт заготовленный ответ.
type recFinder struct {
	hits  []corpus.Hit
	err   error
	calls []string
}

func (f *recFinder) Search(_ context.Context, q, group string, k int) ([]corpus.Hit, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s|%s|%d", q, group, k))
	return f.hits, f.err
}

func TestWorksIndexOrdersBooksAndChapters(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "ethics/x/01.md", "Ethics", "ethics", "x", 1, "genuine")
	chapterFile(t, s.Root, "ethics/ii/02.md", "Ethics", "ethics", "ii", 2, "genuine")
	chapterFile(t, s.Root, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "genuine")
	chapterFile(t, s.Root, "virtues/01.md", "On Virtues and Vices", "virtues", "", 1, "spurious")
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/works", "")
	var works []workRef
	if err := json.Unmarshal(rec.Body.Bytes(), &works); err != nil || len(works) != 2 {
		t.Fatalf("оглавление %s, %v", rec.Body.String(), err)
	}
	e := works[0]
	if e.Slug != "ethics" || len(e.Books) != 2 || e.Books[0].Book != "ii" || e.Books[1].Book != "x" {
		t.Fatalf("книги не по римскому порядку: %+v", e.Books)
	}
	if e.Books[0].Chapters[0].Number != 1 || e.Books[0].Chapters[1].Path != "ethics/ii/02.md" {
		t.Fatalf("главы: %+v", e.Books[0].Chapters)
	}
	if works[1].Authenticity != "spurious" {
		t.Fatalf("подлинность не передана: %+v", works[1])
	}
}

// Оглавление строится по настоящим файлам: ссылка на файл вне corpus/ в нём
// не появляется (глава всё равно не открылась бы), как и глава без
// разборного frontmatter.
func TestWorksIndexSkipsSymlinksAndBrokenChapters(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "genuine")

	outside := t.TempDir()
	chapterFile(t, outside, "leak/01.md", "Leak", "leak", "", 1, "genuine")
	leaked := filepath.Join(outside, "corpus", "leak", "01.md")
	if err := os.MkdirAll(filepath.Join(s.Root, "corpus", "leak"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(leaked, filepath.Join(s.Root, "corpus", "leak", "01.md")); err != nil {
		t.Skipf("символьные ссылки недоступны: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.Root, "corpus", "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "corpus", "broken", "01.md"), []byte("без шапки\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/works", "")
	var works []workRef
	if err := json.Unmarshal(rec.Body.Bytes(), &works); err != nil || len(works) != 1 || works[0].Slug != "ethics" {
		t.Fatalf("в оглавлении только настоящая глава: %s, %v", rec.Body.String(), err)
	}
}

// Корня без corpus/ пользователь не получит пустой библиотекой без
// объяснений: сервер называет причину и не показывает путь на диске.
func TestWorksIndexWithoutCorpusDir(t *testing.T) {
	s := newTestServer(t, nil, nil)
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/works", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "corpus") || strings.Contains(body, s.Root) {
		t.Fatalf("сообщение должно назвать причину и не выдавать путь: %q", body)
	}
}

func TestWorksIndexOmitsEmptyTranslator(t *testing.T) {
	s := newTestServer(t, nil, nil)
	path := filepath.Join(s.Root, "corpus", "poetics", "01.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "---\nwork: \"Poetics\"\nwork_slug: poetics\nbook: null\nchapter: 1\ngroup: rhetoric-poetics\n" +
		"authenticity: genuine\ntranslator: null\nsource: mit\ncite: \"Poetics 1\"\n---\n\nText.\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/works", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "translator") {
		t.Fatalf("пустой переводчик на проводе не нужен: %d %s", rec.Code, rec.Body.String())
	}
	// Работа без книг — одна безымянная книга: интерфейс не рисует для неё заголовок.
	if !strings.Contains(rec.Body.String(), `"book":""`) {
		t.Fatalf("работа без книг: %s", rec.Body.String())
	}
}

func TestChapterPathValidated(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "genuine")
	h := s.Handler()
	if rec := do(t, h, http.MethodGet, "/api/corpus/chapter?path=ethics/ii/01.md", ""); rec.Code != 200 ||
		!strings.Contains(rec.Body.String(), "Text of the chapter.") {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"../config.json", "ethics/../../x/01.md", "/etc/passwd", "ethics/ii/01.txt"} {
		if rec := do(t, h, http.MethodGet, "/api/corpus/chapter?path="+bad, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%q: код %d, ожидался 400", bad, rec.Code)
		}
	}
	if rec := do(t, h, http.MethodGet, "/api/corpus/chapter?path=ethics/ii/09.md", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("нет главы: код %d", rec.Code)
	}
}

func TestChapterResponseFields(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "disputed")
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/chapter?path=ethics/ii/01.md", "")
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"work": "Ethics", "book": "ii", "number": float64(1), "cite": "Ethics II.1", "group": "ethics",
		"authenticity": "disputed", "translator": "W. D. Ross", "path": "ethics/ii/01.md", "text": "Text of the chapter.",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("поле %s = %v, ожидалось %v", k, got[k], v)
		}
	}
}

// Строка из запроса — единственное, что сервер превращает в имя файла, поэтому
// таблица широкая: каждая строка — отдельный способ выйти за corpus/ или
// прочитать не главу. path подставляется в запрос после url.QueryEscape;
// rawQ, если задан, — запрос как есть (процентное кодирование самих точек и
// слэшей: маршрутизатор раскодирует его до обработчика).
func TestChapterPathTraversalTable(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "genuine")
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("SECRET-OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Подкаталог внутри corpus/, который на деле ведёт наружу.
	if err := os.Symlink(outside, filepath.Join(s.Root, "corpus", "escape")); err != nil {
		t.Skipf("символьные ссылки недоступны: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "01.md"), []byte("SECRET-OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Файл-ссылка на файл вне corpus/ с правильным именем главы.
	if err := os.Symlink(secret, filepath.Join(s.Root, "corpus", "ethics", "ii", "02.md")); err != nil {
		t.Fatal(err)
	}
	// Каталог с именем главы.
	if err := os.MkdirAll(filepath.Join(s.Root, "corpus", "ethics", "ii", "03.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ссылка на соседний каталог вне corpus/ в качестве книги.
	if err := os.Symlink(outside, filepath.Join(s.Root, "corpus", "ethics", "iii")); err != nil {
		t.Fatal(err)
	}
	// Ссылка на корень репозитория (родитель corpus/) вместо работы; файл
	// лежит в репозитории, но не в corpus/.
	if err := os.Symlink(s.Root, filepath.Join(s.Root, "corpus", "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Root, "persona"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "persona", "01.md"), []byte("SECRET-OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := []struct {
		name    string
		path    string
		rawQ    string
		noParam bool
		want    int
	}{
		{name: "пустой путь", path: "", want: 400},
		{name: "без параметра", noParam: true, want: 400},
		{name: "точки вверх", path: "../config.json", want: 400},
		{name: "точки вверх в середине", path: "ethics/../../x/01.md", want: 400},
		{name: "точки вверх вокруг главы", path: "../corpus/ethics/ii/01.md", want: 400},
		{name: "одна точка", path: "ethics/./01.md", want: 400},
		{name: "начало с точки-каталога", path: "./ethics/ii/01.md", want: 400},
		{name: "точка в имени работы", path: "ethics.old/01.md", want: 400},
		{name: "две точки как имя", path: "ethics/../01.md", want: 400},
		{name: "точки закодированы %2e", rawQ: "%2e%2e/%2e%2e/etc/passwd.md", want: 400},
		{name: "слэши закодированы %2f", rawQ: "..%2f..%2fetc%2fpasswd", want: 400},
		{name: "точки закодированы дважды", rawQ: "%252e%252e/ethics/ii/01.md", want: 400},
		{name: "абсолютный путь", path: "/etc/passwd", want: 400},
		{name: "абсолютный путь к главе", path: "/ethics/ii/01.md", want: 400},
		{name: "абсолютный путь к настоящему файлу", path: secret, want: 400},
		{name: "слэш в конце", path: "ethics/ii/01.md/", want: 400},
		{name: "двойной слэш", path: "ethics//01.md", want: 400},
		{name: "двойной слэш в начале", path: "//ethics/ii/01.md", want: 400},
		{name: "обратные слэши", path: `ethics\ii\01.md`, want: 400},
		{name: "обратный слэш в конце", path: `ethics/ii\01.md`, want: 400},
		{name: "обратные слэши и точки", path: `..\..\x.md`, want: 400},
		{name: "NUL в конце", path: "ethics/ii/01.md\x00", want: 400},
		{name: "NUL перед расширением", path: "ethics/ii/01\x00.md", want: 400},
		{name: "NUL и чужое расширение", path: "ethics/ii/01.md\x00.txt", want: 400},
		{name: "перевод строки в конце", path: "ethics/ii/01.md\n", want: 400},
		{name: "пробел", path: "ethics/ii/ 01.md", want: 400},
		{name: "не .md", path: "ethics/ii/01.txt", want: 400},
		{name: "двойное расширение", path: "ethics/ii/01.md.txt", want: 400},
		{name: "без расширения", path: "ethics/ii/01", want: 400},
		{name: "расширение в верхнем регистре", path: "ethics/ii/01.MD", want: 400},
		{name: "верхний регистр сегмента", path: "Ethics/ii/01.md", want: 400},
		{name: "каталог без расширения", path: "ethics/ii", want: 400},
		{name: "корень работы", path: "ethics", want: 400},
		{name: "слишком глубоко", path: "ethics/ii/iii/01.md", want: 400},
		{name: "имя главы не число", path: "ethics/ii/intro.md", want: 400},
		{name: "цифры другой письменности", path: "ethics/ii/\uff10\uff11.md", want: 400},
		{name: "диск Windows", path: "C:/x/01.md", want: 400},
		{name: "домашний каталог", path: "~/x/01.md", want: 400},
		{name: "слишком длинное имя работы", path: strings.Repeat("a", 5000) + "/01.md", want: 400},
		{name: "слишком длинное имя главы", path: "a/" + strings.Repeat("1", 5000) + ".md", want: 400},
		{name: "слишком много сегментов", path: strings.Repeat("a/", 300) + "01.md", want: 400},

		{name: "глава есть", path: "ethics/ii/01.md", want: 200},
		{name: "главы нет", path: "ethics/ii/09.md", want: 404},
		{name: "работы нет", path: "nothing/01.md", want: 404},
		{name: "ссылка-файл на файл вне corpus/", path: "ethics/ii/02.md", want: 404},
		{name: "каталог с именем главы", path: "ethics/ii/03.md", want: 404},
		{name: "ссылка-каталог вне corpus/ как работа", path: "escape/01.md", want: 404},
		{name: "ссылка-каталог вне corpus/ как книга", path: "ethics/iii/01.md", want: 404},
		{name: "ссылка на родителя corpus/", path: "up/persona/01.md", want: 404},
	}
	h := s.Handler()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			q := r.rawQ
			if r.rawQ == "" && r.path != "" {
				q = url.QueryEscape(r.path)
			}
			target := "/api/corpus/chapter?path=" + q
			if r.noParam {
				target = "/api/corpus/chapter"
			}
			rec := do(t, h, http.MethodGet, target, "")
			if rec.Code != r.want {
				t.Fatalf("код %d, ожидался %d; тело %q", rec.Code, r.want, rec.Body.String())
			}
			body := rec.Body.String()
			// Ни секрет, ни настоящие пути на диске в ответ не попадают.
			for _, leak := range []string{"SECRET-OUTSIDE", s.Root, outside} {
				if r.want != 200 && strings.Contains(body, leak) {
					t.Fatalf("утечка %q в ответе %q", leak, body)
				}
			}
		})
	}
}

// corpus/ сама может быть ссылкой на другой диск: открывать её корень
// разрешено, запрещено лишь выходить из него.
func TestChapterCorpusDirIsSymlink(t *testing.T) {
	s := newTestServer(t, nil, nil)
	real := t.TempDir()
	chapterFile(t, real, "ethics/ii/01.md", "Ethics", "ethics", "ii", 1, "genuine")
	if err := os.Symlink(filepath.Join(real, "corpus"), filepath.Join(s.Root, "corpus")); err != nil {
		t.Skipf("символьные ссылки недоступны: %v", err)
	}
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/chapter?path=ethics/ii/01.md", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Text of the chapter.") {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
}

// Файл с правильным именем, но без разборного frontmatter — ошибка данных,
// не запроса; причина уходит в журнал сервера, а не в ответ.
func TestChapterUnparsableFile(t *testing.T) {
	s := newTestServer(t, nil, nil)
	path := filepath.Join(s.Root, "corpus", "ethics", "ii", "01.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("без шапки\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/chapter?path=ethics/ii/01.md", "")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), s.Root) {
		t.Fatalf("код %d, %q", rec.Code, rec.Body.String())
	}
}

func TestSearchUsesFinder(t *testing.T) {
	s := newTestServer(t, nil, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=habit", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), ethics.Path) {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
}

func TestSearchViewFields(t *testing.T) {
	long := ethics
	long.Authenticity = "disputed"
	long.Text = "word " + strings.Repeat("слово ", 100)
	s := newTestServer(t, nil, &recFinder{hits: []corpus.Hit{{Chapter: long}}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=habit", "")
	var got []map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 1 {
		t.Fatalf("%s, %v", rec.Body.String(), err)
	}
	h := got[0]
	if h["cite"] != long.Cite || h["path"] != long.Path || h["work"] != long.Work || h["authenticity"] != "disputed" {
		t.Fatalf("поля попадания: %v", h)
	}
	if r := []rune(h["snippet"]); len(r) != 281 || !strings.HasSuffix(h["snippet"], "…") {
		t.Fatalf("выдержка: %d рун, %q", len(r), h["snippet"])
	}
}

func TestSearchEmptyQueryIsEmptyList(t *testing.T) {
	f := &recFinder{}
	s := newTestServer(t, nil, f)
	for _, q := range []string{"", "%20%20"} {
		rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q="+q, "")
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("q=%q: код %d, %q", q, rec.Code, rec.Body.String())
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("пустой запрос дошёл до поиска: %v", f.calls)
	}
}

func TestSearchNoHitsIsEmptyList(t *testing.T) {
	s := newTestServer(t, nil, &recFinder{})
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=nothing", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("код %d, %q", rec.Code, rec.Body.String())
	}
}

func TestSearchPassesQueryGroupAndK(t *testing.T) {
	f := &recFinder{}
	s := newTestServer(t, nil, f)
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q="+url.QueryEscape("  the mean ")+"&group=ethics", "")
	if rec.Code != 200 {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
	// k — из конфига (по умолчанию 6): тот же поиск и те же настройки, что у модели.
	if len(f.calls) != 1 || f.calls[0] != "the mean|ethics|6" {
		t.Fatalf("вызовы поиска: %v", f.calls)
	}
}

// Запрос уходит в qmd как позиционный аргумент: начало с «-» qmd читает как
// собственный флаг (qmd query --help печатает справку, а «-c чужая» меняет
// коллекцию). Оболочки на пути нет, но этого достаточно, чтобы отказать.
func TestSearchRejectsUnsafeInput(t *testing.T) {
	f := &recFinder{}
	s := newTestServer(t, nil, f)
	h := s.Handler()
	rows := []struct{ name, query string }{
		{"флаг в начале", "q=--help"},
		{"короткий флаг", "q=-c"},
		{"флаг после пробелов", "q=" + url.QueryEscape("  --index other")},
		{"только дефис", "q=-"},
		{"NUL в запросе", "q=" + url.QueryEscape("hab\x00it")},
		{"управляющий символ", "q=" + url.QueryEscape("hab\x1bit")},
		{"слишком длинный запрос", "q=" + strings.Repeat("a", 2000)},
		{"группа с мусором", "q=habit&group=" + url.QueryEscape("ethics; rm")},
		{"группа с дефисом в начале", "q=habit&group=--x"},
		{"слишком длинная группа", "q=habit&group=" + strings.Repeat("a", 100)},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			rec := do(t, h, http.MethodGet, "/api/corpus/search?"+r.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("код %d, ожидался 400; тело %q", rec.Code, rec.Body.String())
			}
		})
	}
	if len(f.calls) != 0 {
		t.Fatalf("недопустимый запрос дошёл до поиска: %v", f.calls)
	}
	// Дефис внутри — обычный запрос.
	if rec := do(t, h, http.MethodGet, "/api/corpus/search?q="+url.QueryEscape("self-control"), ""); rec.Code != 200 {
		t.Fatalf("дефис внутри слова: код %d, %s", rec.Code, rec.Body.String())
	}
}

// С настоящим Searcher запрос остаётся одним аргументом: ни разбиения по
// пробелам, ни оболочки, ни метасимволов.
func TestSearchQueryIsSingleArgument(t *testing.T) {
	var got []string
	s := newTestServer(t, nil, &corpus.Searcher{
		Collection: "aristotle", Root: t.TempDir(),
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		},
	})
	q := "habit; rm -rf / $(id) `id` | cat"
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q="+url.QueryEscape(q), "")
	if rec.Code != 200 {
		t.Fatalf("код %d, %s", rec.Code, rec.Body.String())
	}
	// Запрос — последний аргумент, после «--»: так qmd не примет его за флаг.
	if len(got) < 3 || got[0] != "query" || got[len(got)-2] != "--" || got[len(got)-1] != q {
		t.Fatalf("аргументы qmd: %q", got)
	}
}

func TestSearchFailureIs502WithShortMessage(t *testing.T) {
	boom := errors.New("qmd query: exit status 1 в /secret/path/corpus")
	s := newTestServer(t, nil, &recFinder{err: boom})
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=habit", "")
	body := rec.Body.String()
	if rec.Code != http.StatusBadGateway || !strings.Contains(body, "поиск") {
		t.Fatalf("код %d, %q", rec.Code, body)
	}
	if strings.Contains(body, "/secret/path") || strings.Contains(body, "exit status") {
		t.Fatalf("внутренняя ошибка в ответе: %q", body)
	}
}

// Настоящий Searcher без qmd в PATH — путь, которым идёт пользователь без
// индекса: чистый 502 с названной причиной, без падения.
func TestSearchWithoutQmdIs502(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := newTestServer(t, nil, nil)
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=habit", "")
	body := rec.Body.String()
	if rec.Code != http.StatusBadGateway || !strings.Contains(body, "qmd") {
		t.Fatalf("код %d, %q", rec.Code, body)
	}
	if _, err := exec.LookPath("qmd"); err == nil {
		t.Fatal("тест не изолирован от PATH")
	}
}

func TestRoman(t *testing.T) {
	for s, want := range map[string]int{"i": 1, "IV": 4, "ix": 9, "xiv": 14, "": 0, "a": 0} {
		if got := roman(s); got != want {
			t.Errorf("roman(%q) = %d", s, got)
		}
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet("  a \n\n b\tc  ", 10); got != "a b c" {
		t.Errorf("пробелы не свёрнуты: %q", got)
	}
	if got := snippet("абвгд", 3); got != "абв…" {
		t.Errorf("обрезка по рунам: %q", got)
	}
}
