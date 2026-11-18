package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/keys"
)

const fakeKey = "sk-FAKE-кли-ZZ99-a1b2"

func keyPaths(t *testing.T) app.Paths {
	t.Helper()
	dir := t.TempDir()
	return app.Paths{Config: filepath.Join(dir, "config.json"), Keys: filepath.Join(dir, "cfg", "keys.json")}
}

func runKeyWith(t *testing.T, paths app.Paths, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = runKey(args, strings.NewReader(stdin), &out, &errb, paths)
	return code, out.String(), errb.String()
}

func storedKey(t *testing.T, paths app.Paths) string {
	t.Helper()
	k, err := keys.Store{Path: paths.Keys}.Get()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestKeySavesAndPrintsOnlyTheTail(t *testing.T) {
	paths := keyPaths(t)
	code, out, errOut := runKeyWith(t, paths, fakeKey+"\n")
	if code != 0 {
		t.Fatalf("код %d, stderr: %s", code, errOut)
	}
	if got := storedKey(t, paths); got != fakeKey {
		t.Fatalf("сохранён %q", got)
	}
	if strings.TrimSpace(out) != "ключ сохранён (…a1b2)" {
		t.Fatalf("stdout = %q", out)
	}
	// Ни stdout, ни stderr не содержат ключа (кроме четырёх последних знаков).
	for _, s := range []string{out, errOut} {
		if strings.Contains(s, fakeKey) || strings.Contains(s, "ZZ99") || strings.Contains(s, "sk-FAKE") {
			t.Fatalf("в выводе есть ключ: %q", s)
		}
	}
	st, err := os.Stat(paths.Keys)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("права %v, %v", st.Mode().Perm(), err)
	}
}

func TestKeyTrimsAndTakesOnlyFirstLine(t *testing.T) {
	for name, in := range map[string]string{
		"пробелы":            "   " + fakeKey + "  \n",
		"CRLF":               fakeKey + "\r\n",
		"без перевода":       fakeKey,
		"лишние строки":      fakeKey + "\nвторая строка\n",
		"пустые строки ниже": fakeKey + "\n\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			paths := keyPaths(t)
			if code, _, errOut := runKeyWith(t, paths, in); code != 0 {
				t.Fatalf("код %d: %s", code, errOut)
			}
			if got := storedKey(t, paths); got != fakeKey {
				t.Fatalf("сохранён %q", got)
			}
		})
	}
}

func TestKeyShortKeyIsNotShown(t *testing.T) {
	paths := keyPaths(t)
	code, out, _ := runKeyWith(t, paths, "abc\n")
	if code != 0 || strings.TrimSpace(out) != "ключ сохранён (…)" || strings.Contains(out, "abc") {
		t.Fatalf("код %d, stdout = %q", code, out)
	}
}

func TestKeyEmptyInputWritesNothing(t *testing.T) {
	for name, in := range map[string]string{"пусто": "", "перевод строки": "\n", "пробелы": "   \n"} {
		t.Run(name, func(t *testing.T) {
			paths := keyPaths(t)
			code, out, errOut := runKeyWith(t, paths, in)
			if code != 1 || out != "" || !strings.Contains(errOut, "ключ пустой") {
				t.Fatalf("код %d, stdout %q, stderr %q", code, out, errOut)
			}
			if _, err := os.Stat(paths.Keys); !os.IsNotExist(err) {
				t.Fatalf("файл создан: %v", err)
			}
		})
	}
}

func TestKeyRejectsKeyWithInnerSpaceWithoutEchoingIt(t *testing.T) {
	paths := keyPaths(t)
	code, out, errOut := runKeyWith(t, paths, "FAKE-KEY-with space\n")
	if code != 1 {
		t.Fatalf("код %d", code)
	}
	for _, s := range []string{out, errOut} {
		if strings.Contains(s, "FAKE-KEY") {
			t.Fatalf("в выводе есть ключ: %q", s)
		}
	}
	if _, err := os.Stat(paths.Keys); !os.IsNotExist(err) {
		t.Fatalf("файл создан: %v", err)
	}
}

func TestKeyDelete(t *testing.T) {
	paths := keyPaths(t)
	if code, _, _ := runKeyWith(t, paths, fakeKey+"\n"); code != 0 {
		t.Fatal("не сохранился")
	}
	code, out, errOut := runKeyWith(t, paths, "", "-delete")
	if code != 0 || strings.TrimSpace(out) != "ключ удалён" {
		t.Fatalf("код %d, stdout %q, stderr %q", code, out, errOut)
	}
	if got := storedKey(t, paths); got != "" {
		t.Fatalf("ключ остался: %q", got)
	}
	// Повторное удаление — не ошибка. stdin при -delete не читается.
	if code, _, _ := runKeyWith(t, paths, "", "-delete"); code != 0 {
		t.Fatalf("повторное удаление: код %d", code)
	}
}

// -h — справка и код 0, ничего не пишется и stdin не читается.
func TestKeyHelp(t *testing.T) {
	paths := keyPaths(t)
	var out, errb bytes.Buffer
	if code := runKey([]string{"-h"}, failingReader{t}, &out, &errb, paths); code != 0 || !strings.Contains(errb.String(), "aristotle key -delete") {
		t.Fatalf("код %d, stderr %q", code, errb.String())
	}
	if _, err := os.Stat(paths.Keys); !os.IsNotExist(err) {
		t.Fatal("файл ключа создан")
	}
}

func TestKeyDeleteDoesNotReadStdin(t *testing.T) {
	paths := keyPaths(t)
	var out, errb bytes.Buffer
	if code := runKey([]string{"-delete"}, failingReader{t}, &out, &errb, paths); code != 0 {
		t.Fatalf("код %d: %s", code, errb.String())
	}
}

type failingReader struct{ t *testing.T }

func (r failingReader) Read([]byte) (int, error) {
	r.t.Helper()
	r.t.Fatal("stdin прочитан")
	return 0, io.EOF
}

func TestKeyUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"неизвестный флаг":         {"-nope"},
		"ключ аргументом":          {fakeKey},
		"флаг и лишний аргумент":   {"-delete", "x"},
		"флаг со значением не там": {"--delete=maybe"},
	} {
		t.Run(name, func(t *testing.T) {
			paths := keyPaths(t)
			code, out, errOut := runKeyWith(t, paths, fakeKey+"\n", args...)
			if code != 2 {
				t.Fatalf("код %d, ожидался 2", code)
			}
			for _, s := range []string{out, errOut} {
				if strings.Contains(s, "FAKE") {
					t.Fatalf("в выводе есть ключ из аргументов: %q", s)
				}
			}
			if _, err := os.Stat(paths.Keys); !os.IsNotExist(err) {
				t.Fatal("файл ключа создан при ошибке разбора")
			}
		})
	}
}

// Испорченный keys.json не перезаписывается и не удаляется командой; сообщение
// называет файл, но не его содержимое.
func TestKeyRefusesCorruptFile(t *testing.T) {
	paths := keyPaths(t)
	os.MkdirAll(filepath.Dir(paths.Keys), 0o700)
	const body = `{"key": "CORRUPT-FAKE-9999", oops`
	os.WriteFile(paths.Keys, []byte(body), 0o600)
	for _, args := range [][]string{nil, {"-delete"}} {
		code, out, errOut := runKeyWith(t, paths, "NEW-FAKE-KEY\n", args...)
		if code != 1 {
			t.Fatalf("args %v: код %d", args, code)
		}
		for _, s := range []string{out, errOut} {
			if strings.Contains(s, "CORRUPT") || strings.Contains(s, "NEW-FAKE") {
				t.Fatalf("в выводе есть содержимое: %q", s)
			}
		}
		if !strings.Contains(errOut, "keys.json не читается") || !strings.Contains(errOut, paths.Keys) {
			t.Fatalf("сообщение не называет файл: %q", errOut)
		}
		if data, _ := os.ReadFile(paths.Keys); string(data) != body {
			t.Fatalf("файл изменён: %q", data)
		}
	}
}

// Замена ключа: новый заменяет прежний.
func TestKeyReplacesExisting(t *testing.T) {
	paths := keyPaths(t)
	runKeyWith(t, paths, "first-FAKE-1111\n")
	code, out, _ := runKeyWith(t, paths, "second-FAKE-2222\n")
	if code != 0 || strings.TrimSpace(out) != "ключ сохранён (…2222)" {
		t.Fatalf("код %d, stdout %q", code, out)
	}
	if got := storedKey(t, paths); got != "second-FAKE-2222" {
		t.Fatalf("сохранён %q", got)
	}
}

// Не терминал (поток, файл) — приглашения нет: stderr чист, пригодно для
// `echo … | aristotle key`.
func TestKeyNoPromptWhenStdinIsNotATerminal(t *testing.T) {
	paths := keyPaths(t)
	_, _, errOut := runKeyWith(t, paths, fakeKey+"\n")
	if errOut != "" {
		t.Fatalf("stderr = %q", errOut)
	}
	// Настоящий файл, не терминал: тоже без приглашения.
	f, err := os.CreateTemp(t.TempDir(), "in")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(fakeKey + "\n")
	f.Seek(0, 0)
	var out, errb bytes.Buffer
	if code := runKey(nil, f, &out, &errb, keyPaths(t)); code != 0 || errb.Len() != 0 {
		t.Fatalf("код %d, stderr %q", code, errb.String())
	}
}
