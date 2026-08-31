package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
