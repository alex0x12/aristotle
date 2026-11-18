package corpus

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeChapter(t *testing.T, root, rel, authenticity, group, cite string) {
	t.Helper()
	body := "---\nwork: \"W\"\nwork_slug: w\nbook: \"I\"\nchapter: 1\n" +
		"group: " + group + "\nauthenticity: " + authenticity +
		"\ntranslator: null\nsource: mit\ncite: \"" + cite + "\"\n---\n\nтело главы\n"
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeQmd(out string) func(context.Context, ...string) ([]byte, error) {
	return func(context.Context, ...string) ([]byte, error) {
		return []byte(out), nil
	}
}

func TestSearchDropsSpurious(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "econ/i/01.md", "spurious", "ethics", "Economics I.1")

	s := &Searcher{
		Collection: "aristotle",
		Root:       root,
		Run: fakeQmd(`[
		  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
		  {"file":"qmd://aristotle/econ/i/01.md","score":0.8}
		]`),
	}
	hits, err := s.Search(context.Background(), "happiness", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("получено %d попаданий, ожидалось 1", len(hits))
	}
	if hits[0].Chapter.Cite != "NE I.1" {
		t.Fatalf("Cite = %q", hits[0].Chapter.Cite)
	}
}

func TestSearchFiltersByGroup(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")
	writeChapter(t, root, "ha/i/01.md", "genuine", "biology", "HA I.1")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/ha/i/01.md","score":0.85}
	]`)}
	hits, err := s.Search(context.Background(), "х", "ethics", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chapter.Group != "ethics" {
		t.Fatalf("фильтр по group не сработал: %+v", hits)
	}
}

func TestSearchPassesCollectionAndLimit(t *testing.T) {
	var got []string
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		}}
	if _, err := s.Search(context.Background(), "happiness", "", 6); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"query", "happiness", "-c aristotle", "--json", "-n 6"} {
		if !strings.Contains(joined, strings.ReplaceAll(want, " ", " ")) {
			t.Errorf("в аргументах нет %q: %v", want, got)
		}
	}
}

// TestSearchQueryIsLastAfterTerminator: запрос — позиционный аргумент qmd, и
// qmd читает начало с «-» как флаг (`--help` печатает справку, `-c чужая`
// подменяет коллекцию). Поэтому все флаги идут первыми, а запрос — последним,
// после «--», как есть; ничего из запроса до «--» не попадает, а «-c» и
// коллекция — собственные у Searcher.
func TestSearchQueryIsLastAfterTerminator(t *testing.T) {
	for _, q := range []string{"--help", "-c other", "-n 1", "--json", "-", "happiness", "две  слова; $(id)"} {
		t.Run(q, func(t *testing.T) {
			var got []string
			s := &Searcher{Collection: "mine", Root: t.TempDir(),
				Run: func(_ context.Context, args ...string) ([]byte, error) {
					got = args
					return []byte("[]"), nil
				}}
			if _, err := s.Search(context.Background(), q, "", 4); err != nil {
				t.Fatal(err)
			}
			want := []string{"query", "-c", "mine", "--json", "-n", "4", "--", q}
			if !slices.Equal(got, want) {
				t.Fatalf("аргументы qmd:\n got  %q\n want %q", got, want)
			}
		})
	}
}

// TestSearchSkipsMissingFile: индекс отстал от диска для ОДНОГО файла из
// нескольких — остальные попадания читаются нормально, отстающий файл
// молча пропускается, ошибки нет. Полный отказ чтения (все файлы недоступны)
// — отдельный случай, см. TestSearchAllHitsUnreadableIsError: там это уже
// не тихий пропуск, а ошибка.
func TestSearchSkipsMissingFile(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/нет/такого.md","score":0.8}
	]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err != nil {
		t.Fatalf("пропавший файл не должен ронять поиск, если другие прочлись: %v", err)
	}
	if len(hits) != 1 || hits[0].Chapter.Cite != "NE I.1" {
		t.Fatalf("получено %+v", hits)
	}
}

// TestSearchAllHitsUnreadableIsError: qmd вернул попадания, но ни одно не
// прочиталось — это почти наверняка неверный корень корпуса, а не честная
// пустая выдача. Пользователь не должен получить агента, который тихо
// отвечает "в моих сочинениях об этом ничего нет" из-за опечатки в пути.
func TestSearchAllHitsUnreadableIsError(t *testing.T) {
	root := t.TempDir()
	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/нет/такого.md","score":0.9},
	  {"file":"qmd://aristotle/тоже/нет.md","score":0.8}
	]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err == nil {
		t.Fatalf("ожидалась ошибка при полном отказе чтения, получено %d попаданий", len(hits))
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("ошибка не называет корень корпуса %q: %v", root, err)
	}
}

// TestSearchGenuinelyEmptyIsNotError: qmd вернул ноль попаданий — это честная
// пустота, а не сбой чтения, ошибки быть не должно.
func TestSearchGenuinelyEmptyIsNotError(t *testing.T) {
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(), Run: fakeQmd(`[]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err != nil {
		t.Fatalf("нулевая выдача qmd — не ошибка: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("получено %d попаданий", len(hits))
	}
}

// TestSearchRejectsPathTraversal: выдача qmd пытается увести чтение за
// пределы корня корпуса через "..". Такое попадание пропускается, даже если
// по этому пути реально лежит файл.
func TestSearchRejectsPathTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "corpus")
	writeChapter(t, root, "ne/i/01.md", "genuine", "ethics", "NE I.1")

	secretBody := "---\nwork: \"S\"\nwork_slug: s\nbook: null\nchapter: 1\n" +
		"group: ethics\nauthenticity: genuine\ntranslator: null\nsource: mit\n" +
		"cite: \"Secret\"\n---\n\nсекрет за пределами корпуса\n"
	if err := os.WriteFile(filepath.Join(parent, "secret.md"), []byte(secretBody), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/../secret.md","score":0.8}
	]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Chapter.Cite != "NE I.1" {
		t.Fatalf("попадание за пределами корпуса не должно было пройти: %+v", hits)
	}
}

// TestSearchDefaultsKWhenNonPositive: k <= 0 должно превращаться в 6, а не
// проходить в qmd как есть. До этого теста ветка по умолчанию не
// исполнялась ни разу — все прочие тесты передают положительное k.
func TestSearchDefaultsKWhenNonPositive(t *testing.T) {
	var got []string
	s := &Searcher{Collection: "aristotle", Root: t.TempDir(),
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			got = args
			return []byte("[]"), nil
		}}
	if _, err := s.Search(context.Background(), "happiness", "", 0); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-n 6") {
		t.Errorf("k <= 0 не подставило значение по умолчанию 6: %v", got)
	}
}

// writeUnparsableChapter кладёт файл главы, который читается с диска, но не
// разбирается: без обязательного поля group. Отличие от writeChapter важно —
// проверяет ветку "read > 0 && parsed == 0" отдельно от "read == 0".
func writeUnparsableChapter(t *testing.T, root, rel string) {
	t.Helper()
	body := "---\nwork: \"W\"\nwork_slug: w\nbook: \"I\"\nchapter: 1\n" +
		"authenticity: genuine\ntranslator: null\nsource: mit\ncite: \"C\"\n---\n\nтело\n"
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSearchAllReadButNoneParsedIsError: главы читаются с диска, но ни одна
// не разбирается (испорченный frontmatter — здесь нет поля group). Это не
// "неверный корень корпуса" (файлы на месте), а подозрение на то, что
// корпус собран не тем конвейером или разошёлся со схемой. Тоже ошибка,
// а не пустая выдача — тот же дефект, что и правило 3, просто этажом ниже.
func TestSearchAllReadButNoneParsedIsError(t *testing.T) {
	root := t.TempDir()
	writeUnparsableChapter(t, root, "ne/i/01.md")
	writeUnparsableChapter(t, root, "ne/i/02.md")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ne/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/ne/i/02.md","score":0.8}
	]`)}
	hits, err := s.Search(context.Background(), "x", "", 5)
	if err == nil {
		t.Fatalf("ожидалась ошибка: главы читаются, но не разбираются; получено %d попаданий", len(hits))
	}
}

// TestSearchAllParsedButFilteredIsEmptyNotError: главы читаются и
// разбираются успешно (подлинные, со всеми обязательными полями), но
// целиком отсеиваются фильтром по group. Это законная пустота, а не сбой —
// счётчик разобранных растёт ДО фильтров spurious/group именно ради этого
// случая. Важнее первого теста: страховка от того, что починка правила 3
// перегнёт палку и начнёт ошибаться на честном "в этой группе ничего нет".
func TestSearchAllParsedButFilteredIsEmptyNotError(t *testing.T) {
	root := t.TempDir()
	writeChapter(t, root, "ha/i/01.md", "genuine", "biology", "HA I.1")
	writeChapter(t, root, "ha/i/02.md", "genuine", "biology", "HA I.2")

	s := &Searcher{Collection: "aristotle", Root: root, Run: fakeQmd(`[
	  {"file":"qmd://aristotle/ha/i/01.md","score":0.9},
	  {"file":"qmd://aristotle/ha/i/02.md","score":0.8}
	]`)}
	hits, err := s.Search(context.Background(), "x", "ethics", 5)
	if err != nil {
		t.Fatalf("выдача, целиком отсеянная фильтром по group, не должна давать ошибку: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("получено %d попаданий, ожидалось 0", len(hits))
	}
}
