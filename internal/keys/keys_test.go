package keys

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeKey = "sk-FAKE-секрет-ZZ9f-a1b2"

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "aristotle", "keys.json")}
}

func TestEmptyStoreHasNoKey(t *testing.T) {
	s := newStore(t)
	if k, err := s.Get(); err != nil || k != "" {
		t.Fatalf("нет файла: %q, %v", k, err)
	}
	if err := s.Delete(); err != nil {
		t.Fatalf("удаление несуществующего ключа — не ошибка: %v", err)
	}
}

func TestSetGetDelete(t *testing.T) {
	s := newStore(t)
	if err := s.Set("  " + fakeKey + "\n"); err != nil {
		t.Fatal(err)
	}
	if k, err := s.Get(); err != nil || k != fakeKey {
		t.Fatalf("ключ = %q, %v (пробелы по краям должны срезаться)", k, err)
	}
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if k, err := s.Get(); err != nil || k != "" {
		t.Fatalf("после удаления: %q, %v", k, err)
	}
	if _, err := os.Stat(s.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("файл после удаления ключа должен исчезнуть: %v", err)
	}
	// Повторное удаление и повторная установка работают.
	if err := s.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("other-FAKE-key"); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Get(); k != "other-FAKE-key" {
		t.Fatalf("ключ = %q", k)
	}
}

func TestSetReplacesKey(t *testing.T) {
	s := newStore(t)
	s.Set("first-FAKE")
	if err := s.Set("second-FAKE"); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Get(); k != "second-FAKE" {
		t.Fatalf("ключ = %q", k)
	}
}

func TestFileFormatAndModes(t *testing.T) {
	s := newStore(t)
	if err := s.Set(fakeKey); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(s.Path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права файла %v, %v", st.Mode().Perm(), err)
	}
	dst, err := os.Stat(filepath.Dir(s.Path))
	if err != nil || dst.Mode().Perm() != 0o700 {
		t.Fatalf("права каталога %v, %v", dst.Mode().Perm(), err)
	}
	data, _ := os.ReadFile(s.Path)
	if !strings.Contains(string(data), `"key"`) || !strings.Contains(string(data), fakeKey) {
		t.Fatalf("формат файла: %s", data)
	}
	// Перезапись не расширяет права и не оставляет временных файлов.
	if err := s.Set("second"); err != nil {
		t.Fatal(err)
	}
	st, _ = os.Stat(s.Path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("права после замены %v", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(s.Path))
	if len(entries) != 1 {
		t.Fatalf("в каталоге лишнее: %v", entries)
	}
}

func TestSetRejectsBadKeysWithoutWriting(t *testing.T) {
	long := strings.Repeat("k", 4097)
	for name, raw := range map[string]string{
		"пустой":          "",
		"пробелы":         "   \n",
		"пробел внутри":   "FAKE one",
		"перевод строки":  "FAKE\nKEY",
		"возврат каретки": "FAKE\rKEY",
		"NUL":             "FAKE\x00KEY",
		"табуляция":       "FAKE\tKEY",
		"слишком длинный": long,
	} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			err := s.Set(raw)
			if err == nil {
				t.Fatal("ожидалась ошибка")
			}
			if strings.Contains(err.Error(), "FAKE") || strings.Contains(err.Error(), "kkkk") {
				t.Fatalf("ошибка повторяет ключ: %v", err)
			}
			if _, statErr := os.Stat(s.Path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("файл создан несмотря на отказ: %v", statErr)
			}
		})
	}
	// Ровно 4096 знаков — ещё можно.
	s := newStore(t)
	if err := s.Set(strings.Repeat("k", 4096)); err != nil {
		t.Fatalf("4096 знаков отвергнуты: %v", err)
	}
}

func TestNormalizeSentinels(t *testing.T) {
	if _, err := Normalize(" "); !errors.Is(err, ErrEmpty) {
		t.Fatalf("пустой: %v", err)
	}
	if _, err := Normalize(strings.Repeat("k", 4097)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("длинный: %v", err)
	}
	if _, err := Normalize("a b"); !errors.Is(err, ErrBadChars) {
		t.Fatalf("пробел: %v", err)
	}
	if k, err := Normalize("  ok-key \n"); err != nil || k != "ok-key" {
		t.Fatalf("%q, %v", k, err)
	}
}

// Старый формат ({"profiles": …}) — допустимый JSON без поля key: это «ключа
// нет», а не ошибка (миграции нет). Установка нового ключа заменяет файл.
func TestOldFormatMeansNoKey(t *testing.T) {
	s := newStore(t)
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	old := `{"profiles":{"deepseek":"OLD-FAKE-KEY-1111"}}`
	os.WriteFile(s.Path, []byte(old), 0o600)
	if k, err := s.Get(); err != nil || k != "" {
		t.Fatalf("старый формат: %q, %v", k, err)
	}
	if err := s.Set(fakeKey); err != nil {
		t.Fatal(err)
	}
	if k, _ := s.Get(); k != fakeKey {
		t.Fatalf("ключ = %q", k)
	}
}

// Испорченный файл не перезаписывается и не удаляется ни при каком вызове: в
// нём может лежать единственная копия ключа, которую человек починит руками.
func TestCorruptFileIsNeverTouched(t *testing.T) {
	garbage := map[string]string{
		"не JSON":          `{"key": "CORRUPT-FAKE-9999", oops`,
		"пустой файл":      ``,
		"массив":           `["CORRUPT-FAKE-9999"]`,
		"строка":           `"CORRUPT-FAKE-9999"`,
		"ключ не строка":   `{"key": 42}`,
		"лишнее после":     `{"key":"CORRUPT-FAKE-9999"} xx`,
		"ключ с переводом": "{\"key\":\"CORRUPT\\nFAKE\"}",
	}
	for name, body := range garbage {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			os.MkdirAll(filepath.Dir(s.Path), 0o700)
			os.WriteFile(s.Path, []byte(body), 0o600)

			check := func(what string, err error) {
				t.Helper()
				var ue *UnreadableError
				if !errors.As(err, &ue) {
					t.Fatalf("%s: ожидалась *UnreadableError, получено %v", what, err)
				}
				if err.Error() != UnreadableText {
					t.Fatalf("%s: текст ошибки %q, ожидалось фиксированное %q", what, err.Error(), UnreadableText)
				}
				// Ни самой ошибкой, ни её причиной (она идёт в журнал сервера)
				// не выдаётся содержимое файла.
				for e := error(err); e != nil; e = errors.Unwrap(e) {
					if strings.Contains(e.Error(), "CORRUPT") {
						t.Fatalf("%s: в ошибке есть содержимое файла: %v", what, e)
					}
				}
			}
			_, err := s.Get()
			check("Get", err)
			check("Set", s.Set("NEW-FAKE-KEY"))
			check("Delete", s.Delete())

			after, _ := os.ReadFile(s.Path)
			if !bytes.Equal(after, []byte(body)) {
				t.Fatalf("файл изменился: %q", after)
			}
			entries, _ := os.ReadDir(filepath.Dir(s.Path))
			if len(entries) != 1 {
				t.Fatalf("лишние файлы: %v", entries)
			}
		})
	}
}

// Файл есть, но каталог не читается — тоже «не читается», а не «ключа нет».
func TestUnreadablePathIsAnError(t *testing.T) {
	dir := t.TempDir()
	s := Store{Path: dir} // каталог вместо файла
	if _, err := s.Get(); err == nil {
		t.Fatal("каталог вместо файла должен давать ошибку")
	}
}

func TestHintShowsOnlyTail(t *testing.T) {
	if got := Hint("sk-ant-секрет-a1b2"); got != "…a1b2" {
		t.Fatalf("Hint = %q", got)
	}
	for _, short := range []string{"abc", "abcd", ""} {
		if got := Hint(short); got != "…" || (short != "" && strings.Contains(got, short)) {
			t.Fatalf("короткий ключ %q не показывается: %q", short, got)
		}
	}
	if got := Hint("abcde"); got != "…bcde" {
		t.Fatalf("Hint(5 знаков) = %q", got)
	}
}
