package corpus

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusDir — корпус самого репозитория. Тесты таблицы названий читают его
// напрямую: она нужна ровно для этих файлов, и расхождение с ними — единственная
// ошибка, которую здесь стоит ловить. Нет corpus/ — тест падает, а не
// пропускается: молчаливый пропуск оставил бы таблицу непроверенной.
const corpusDir = "../../corpus"

// readCorpus разбирает все главы корпуса репозитория.
func readCorpus(t *testing.T) []Chapter {
	t.Helper()
	if fi, err := os.Stat(corpusDir); err != nil || !fi.IsDir() {
		t.Fatalf("нет каталога %s (тест таблицы названий читает корпус репозитория): %v", corpusDir, err)
	}
	var out []Chapter
	err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ch, err := ParseChapter(path, data)
		if err != nil {
			t.Errorf("глава корпуса не разобралась: %v", err)
			return nil
		}
		out = append(out, ch)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("в %s нет ни одной главы", corpusDir)
	}
	return out
}

// Каждый каталог корпуса есть в таблице, и в таблице нет записей, которым не
// соответствует каталог: мёртвая запись пережила бы удаление работы незаметно.
func TestTitlesCoverCorpusDirectories(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatalf("нет каталога %s: %v", corpusDir, err)
	}
	dirs := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}
	if len(dirs) == 0 {
		t.Fatalf("в %s нет ни одного каталога работы", corpusDir)
	}
	inTable := map[string]bool{}
	for _, w := range titles {
		if inTable[w.Slug] {
			t.Errorf("slug %q в таблице дважды", w.Slug)
		}
		inTable[w.Slug] = true
		if !dirs[w.Slug] {
			t.Errorf("запись %q: каталога corpus/%s нет", w.Slug, w.Slug)
		}
		if w.En == "" || w.Ru == "" {
			t.Errorf("запись %q с пустым названием: En=%q Ru=%q", w.Slug, w.En, w.Ru)
		}
	}
	for d := range dirs {
		if !inTable[d] {
			t.Errorf("работы %q нет в таблице названий", d)
		}
	}
}

// En дублирует поле work из frontmatter, чтобы ссылку можно было перевести без
// обращения к диску. Расхождение таблицы с файлами — ошибка таблицы.
func TestTitlesEnglishMatchesFrontmatter(t *testing.T) {
	for _, ch := range readCorpus(t) {
		w, ok := titleBySlug(ch.WorkSlug)
		if !ok {
			t.Errorf("%s: slug %q не в таблице", ch.Path, ch.WorkSlug)
			continue
		}
		if w.En != ch.Work {
			t.Errorf("%s: En=%q, а work во frontmatter=%q", ch.Path, w.En, ch.Work)
		}
		// Первый каталог под corpus/ — это slug работы: таблица ищется по нему.
		rel, err := filepath.Rel(corpusDir, ch.Path)
		if err != nil {
			t.Fatal(err)
		}
		if dir, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); dir != ch.WorkSlug {
			t.Errorf("%s: каталог %q не совпадает с work_slug %q", ch.Path, dir, ch.WorkSlug)
		}
	}
}

// Правило префикса проверяется на настоящих данных, всех главах корпуса:
// и на работах без книг («On Longevity and Shortness of Life 6»), и на
// составном названии «On Youth and Old Age, On Life and Death, On Breathing».
func TestCiteRuOnEveryCorpusChapter(t *testing.T) {
	chapters := readCorpus(t)
	for _, ch := range chapters {
		ru, ok := CiteRu(ch.Cite)
		if !ok {
			t.Errorf("%s: CiteRu(%q) не нашла работу", ch.Path, ch.Cite)
			continue
		}
		title, _ := TitleRu(ch.WorkSlug)
		if !strings.HasPrefix(ru, title+" ") {
			t.Errorf("%s: %q не начинается с %q", ch.Path, ru, title)
			continue
		}
		wantRest := strings.TrimPrefix(ch.Cite, ch.Work+" ")
		if wantRest == ch.Cite {
			t.Errorf("%s: cite %q не начинается с английского названия %q", ch.Path, ch.Cite, ch.Work)
			continue
		}
		if rest := strings.TrimPrefix(ru, title+" "); rest != wantRest {
			t.Errorf("%s: нумерация %q, ожидалась %q", ch.Path, rest, wantRest)
		}
		// Разбор по частям согласован с цельной формой.
		if rt, num, ok := SplitCiteRu(ch.Cite); !ok || rt != title || num != wantRest || rt+" "+num != ru {
			t.Errorf("%s: SplitCiteRu = %q, %q, %v расходится с CiteRu = %q", ch.Path, rt, num, ok, ru)
		}
	}
	t.Logf("глав проверено: %d", len(chapters))
}

func TestTitleRu(t *testing.T) {
	for _, c := range []struct {
		slug string
		want string
		ok   bool
	}{
		{"nicomachean-ethics", "Никомахова этика", true},
		{"politics", "Политика", true},
		{"on-the-soul", "О душе", true},
		{"no-such-work", "", false},
		{"", "", false},
		{"Politics", "", false}, // строго: slug — строчные
	} {
		got, ok := TitleRu(c.slug)
		if got != c.want || ok != c.ok {
			t.Errorf("TitleRu(%q) = %q, %v; ожидалось %q, %v", c.slug, got, ok, c.want, c.ok)
		}
	}
}

func TestWorkRu(t *testing.T) {
	for _, c := range []struct {
		en   string
		want string
		ok   bool
	}{
		{"Nicomachean Ethics", "Никомахова этика", true},
		{"On Youth and Old Age, On Life and Death, On Breathing", "О юности и старости, о жизни и смерти и о дыхании", true},
		{"nicomachean ethics", "", false}, // регистр значим
		{"Nicomachean Ethics ", "", false},
		{"", "", false},
	} {
		got, ok := WorkRu(c.en)
		if got != c.want || ok != c.ok {
			t.Errorf("WorkRu(%q) = %q, %v; ожидалось %q, %v", c.en, got, ok, c.want, c.ok)
		}
	}
}

func TestCiteRu(t *testing.T) {
	for _, c := range []struct {
		name, cite, want string
		ok               bool
	}{
		{"книга и глава", "Nicomachean Ethics II.1", "Никомахова этика II.1", true},
		{"работа без книг", "On Longevity and Shortness of Life 6", "О долготе и краткости жизни 6", true},
		{"составное название", "On Youth and Old Age, On Life and Death, On Breathing 23",
			"О юности и старости, о жизни и смерти и о дыхании 23", true},
		{"название с артиклем", "The History of Animals VI.23", "История животных VI.23", true},
		{"нумерация произвольная — остаток берётся как есть", "Nicomachean Ethics II.1, II.6", "Никомахова этика II.1, II.6", true},
		{"неизвестная работа", "On the Cosmos 3", "", false},
		{"пустая строка", "", "", false},
		{"только название, без нумерации", "Politics", "", false},
		{"название и пробел, нумерации нет", "Politics ", "", false},
		{"два пробела — не «ровно один»", "Politics  II.1", "", false},
		{"регистр значим", "nicomachean ethics II.1", "", false},
		{"пробел в начале не срезается", " Politics II.1", "", false},
		{"префикс без границы слова", "Politicsx II.1", "", false},
		{"русская ссылка", "Политика II.1", "", false},
		{"после названия не нумерация", "Politics Book II", "", false},
		{"слово на «C» — не римская цифра", "Politics Chapter 1", "", false},
		{"одна книга без главы — не ссылка корпуса", "Nicomachean Ethics II", "", false},
		{"хвост после нумерации сохраняется", "Politics VIII.7 (1342a)", "Политика VIII.7 (1342a)", true},
	} {
		got, ok := CiteRu(c.cite)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: CiteRu(%q) = %q, %v; ожидалось %q, %v", c.name, c.cite, got, ok, c.want, c.ok)
		}
	}
}

// В настоящей таблице нет работы, название которой — собственное начало
// другой: «On Sleep» при «On Sleep and Sleeplessness». Ловушка собирается на
// таблице самого теста; побеждает самое длинное название.
func TestCiteRuLongestPrefixWins(t *testing.T) {
	table := []WorkTitle{
		{Slug: "short", En: "On Sleep", Ru: "О сне"},
		{Slug: "long", En: "On Sleep and Sleeplessness", Ru: "О сне и бодрствовании"},
	}
	for _, c := range []struct {
		cite, want string
		ok         bool
	}{
		{"On Sleep and Sleeplessness 2", "О сне и бодрствовании 2", true},
		{"On Sleep 2", "О сне 2", true},
		// Название длинной работы без нумерации не должно достаться короткой.
		{"On Sleep and Sleeplessness", "", false},
		{"On Sleep and", "", false},
		{"On Sleep and Sleeplessness  2", "", false},
	} {
		got, ok := citeRu(table, c.cite)
		if got != c.want || ok != c.ok {
			t.Errorf("citeRu(%q) = %q, %v; ожидалось %q, %v", c.cite, got, ok, c.want, c.ok)
		}
	}
	// Порядок записей на результат не влияет.
	rev := []WorkTitle{table[1], table[0]}
	if got, _ := citeRu(rev, "On Sleep and Sleeplessness 2"); got != "О сне и бодрствовании 2" {
		t.Errorf("при обратном порядке таблицы: %q", got)
	}
	// Пустое английское название никогда не совпадает: иначе оно «узнавало»
	// бы любую строку, начинающуюся с пробела.
	if got, ok := citeRu([]WorkTitle{{Slug: "x", En: "", Ru: "Икс"}}, " 1"); ok {
		t.Errorf("запись с пустым En совпала: %q", got)
	}
}

// Разбор нумерации: сайт показывает «название · нумерация», и делить строку
// по пробелу на клиенте было бы гаданием — делит сервер.
func TestSplitCiteRu(t *testing.T) {
	ru, num, ok := SplitCiteRu("Nicomachean Ethics II.1")
	if !ok || ru != "Никомахова этика" || num != "II.1" {
		t.Errorf("SplitCiteRu = %q, %q, %v", ru, num, ok)
	}
	if ru, num, ok := SplitCiteRu("Nicomachean Ethics"); ok || ru != "" || num != "" {
		t.Errorf("без нумерации разбор не удаётся: %q, %q, %v", ru, num, ok)
	}
	if ru, num, ok := SplitCiteRu("On Youth and Old Age, On Life and Death, On Breathing 23"); !ok ||
		ru != "О юности и старости, о жизни и смерти и о дыхании" || num != "23" {
		t.Errorf("составное название: %q, %q, %v", ru, num, ok)
	}
}
