package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesWithModeAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "нет", "ещё", "keys.json")
	if err := WriteAtomic(path, []byte("один"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("два"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "два" {
		t.Fatalf("содержимое %q, ошибка %v", data, err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("права %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("временный файл остался: %v", entries)
	}
}
