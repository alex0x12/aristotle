package keys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSetGetDelete(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "aristotle", "keys.json")}
	if k, err := s.Get("claude"); err != nil || k != "" {
		t.Fatalf("пустое хранилище: %q, %v", k, err)
	}
	if err := s.Set("claude", "  sk-ant-секрет-1234  "); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("deepseek", "sk-ds-5678"); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Get("claude"); k != "sk-ant-секрет-1234" {
		t.Fatalf("ключ = %q (пробелы по краям должны срезаться)", k)
	}
	st, err := os.Stat(s.Path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права %v, %v", st.Mode().Perm(), err)
	}
	if err := s.Delete("claude"); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Get("claude"); k != "" {
		t.Fatalf("после удаления ключ = %q", k)
	}
	if k, _ := s.Get("deepseek"); k != "sk-ds-5678" {
		t.Fatal("удаление задело чужой ключ")
	}
}

func TestStoreRejectsEmptyKey(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "keys.json")}
	if err := s.Set("claude", "   "); err == nil {
		t.Fatal("пустой ключ должен отвергаться")
	}
}

func TestHintShowsOnlyTail(t *testing.T) {
	if got := Hint("sk-ant-секрет-a1b2"); got != "…a1b2" {
		t.Fatalf("Hint = %q", got)
	}
	if got := Hint("abc"); got != "…" || strings.Contains(got, "abc") {
		t.Fatalf("короткий ключ не показывается: %q", got)
	}
}
