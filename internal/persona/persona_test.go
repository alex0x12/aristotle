package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/corpus"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOrderIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "02-voice.md", "голос")
	write(t, dir, "01-who.md", "био")
	write(t, dir, "10-late.md", "поздний")

	first, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Load(dir)
	if first != second {
		t.Fatal("сборка обязана быть побайтно повторяемой: иначе кэш промпта мёртв")
	}
	if !strings.HasPrefix(first, "био") {
		t.Fatalf("порядок нарушен: %q", first[:20])
	}
	if strings.Index(first, "голос") > strings.Index(first, "поздний") {
		t.Fatal("10-late.md должен идти после 02-voice.md")
	}
}

func TestLoadIgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "01-who.md", "био")
	write(t, dir, "заметки.txt", "мусор")
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "мусор") {
		t.Fatal("в промпт попал не-markdown файл")
	}
}

func TestLoadEmptyDirFails(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("пустой каталог персоны — ошибка, а не пустой промпт")
	}
}

// Открытый в редакторе файл персоны оставляет рядом висячий симлинк
// .#04-doctrine.md: он проходит фильтр по расширению, а читается с ошибкой.
func TestLoadSkipsDotFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "01-who.md", "био")
	if err := os.Symlink("никуда", filepath.Join(dir, ".#01-who.md")); err != nil {
		t.Skipf("симлинки недоступны: %v", err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("висячий файл редактора не должен ронять сборку: %v", err)
	}
	if out != "био" {
		t.Fatalf("собралось не то: %q", out)
	}
}

// Тесты выше работают на временных каталогах и не заметят, если файл персоны
// переименуют или удалят. Этот смотрит на настоящий persona/.
func TestRealPersonaAssembles(t *testing.T) {
	dir := filepath.Join("..", "..", "persona")
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"01-who.md", "02-voice.md", "03-method.md",
		"04-doctrine.md", "05-contract.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("файл персоны пропал: %s", name)
		}
	}

	// Промпт входит в кэшируемый префикс каждого запроса, потолок — 18 КБ.
	const maxBytes = 18 * 1024
	if len(out) > maxBytes {
		t.Errorf("промпт разросся до %d байт при потолке %d", len(out), maxBytes)
	}

	// У пользователя нет шрифта для эмодзи, стрелок и прочих значков: всё,
	// что нужно персоне (кириллица, греческий, кавычки, тире), лежит ниже U+2100.
	for i, r := range out {
		if r > 0x2100 {
			t.Fatalf("посторонний знак U+%04X на позиции %d", r, i)
		}
	}

	// Набор маркеров закрыт в internal/render; персона обязана называть все шесть.
	for _, marker := range []string{
		">> cite:", ">> en:", ">> ru:",
		">> вывод:", ">> [факт]:", ">> [посылка]:",
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("маркер %q не дожил до собранного промпта", marker)
		}
	}
}

// Агент называет «Метеорологику» так же, как подписывает её интерфейс: русское
// название берётся из таблицы корпуса. Остальные названия персоны сюда не
// сведены нарочно — они сверены с таблицей вручную, а не тестом.
func TestRealPersonaNamesMeteorologyLikeTheTable(t *testing.T) {
	out, err := Load(filepath.Join("..", "..", "persona"))
	if err != nil {
		t.Fatal(err)
	}
	ru, ok := corpus.TitleRu("meteorology")
	if !ok {
		t.Fatal("meteorology нет в таблице названий корпуса")
	}
	if !strings.Contains(out, "«"+ru+"»") {
		t.Errorf("в персоне нет «%s» — так называет работу интерфейс", ru)
	}
	if strings.Contains(out, "Метеорология") {
		t.Error("в персоне осталось «Метеорология»: в «Мысли» т.3 это «Метеорологика»")
	}
}
