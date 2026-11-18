package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/keys"
)

const keyUsage = `использование: aristotle key [-delete]

  aristotle key           сохранить ключ API: одна строка со стандартного ввода
  aristotle key -delete   удалить сохранённый ключ

Ключ лежит только в ~/.config/aristotle/keys.json (права 0600); переменные
окружения для ключа не читаются. Ключ не принимается аргументом командной
строки: он остался бы в истории оболочки.
`

// maxKeyLine — сколько байт стандартного ввода читается ради ключа: настоящий
// ключ на порядок короче, а бесконечный поток не должен копиться в памяти.
const maxKeyLine = 1 << 16

// runKeyCLI — подкоманда `aristotle key` с настоящими потоками и путями.
func runKeyCLI(args []string) int {
	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return runKey(args, os.Stdin, os.Stdout, os.Stderr, paths)
}

// runKey сохраняет или удаляет ключ. Код возврата: 0 — готово, 1 — не вышло,
// 2 — неверное обращение. Ключ нигде не печатается: в stdout — только его
// последние четыре знака, в сообщениях об ошибках — ничего из ввода.
func runKey(args []string, stdin io.Reader, stdout, stderr io.Writer, paths app.Paths) int {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // текст ошибки flag повторил бы аргумент, а им может оказаться ключ
	del := fs.Bool("delete", false, "удалить сохранённый ключ")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stderr, keyUsage)
			return 0
		}
		fmt.Fprintln(stderr, "ошибка: неизвестный или неверный флаг")
		fmt.Fprint(stderr, keyUsage)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "ошибка: ключ не принимается аргументом командной строки — введите его на стандартный ввод")
		fmt.Fprint(stderr, keyUsage)
		return 2
	}
	store := keys.Store{Path: paths.Keys}

	if *del {
		if err := store.Delete(); err != nil {
			return keyFail(stderr, paths, err)
		}
		fmt.Fprintln(stdout, "ключ удалён")
		return 0
	}

	raw, err := readKey(stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка: ввод не прочитан")
		return 1
	}
	// Проверка до всякой записи: плохой ключ не должен даже создать файл.
	key, err := keys.Normalize(raw)
	if err != nil {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	if err := store.Set(key); err != nil {
		return keyFail(stderr, paths, err)
	}
	fmt.Fprintf(stdout, "ключ сохранён (%s)\n", keys.Hint(key))
	return 0
}

// keyFail печатает причину отказа. Для испорченного keys.json называет файл
// (в командной строке это уместно), но не его содержимое.
func keyFail(stderr io.Writer, paths app.Paths, err error) int {
	var ue *keys.UnreadableError
	if errors.As(err, &ue) {
		fmt.Fprintf(stderr, "ошибка: %s (%s): ключ не изменён — исправьте файл или удалите его вручную\n",
			keys.UnreadableText, paths.Keys)
		return 1
	}
	if errors.Is(err, keys.ErrEmpty) || errors.Is(err, keys.ErrTooLong) || errors.Is(err, keys.ErrBadChars) {
		fmt.Fprintln(stderr, "ошибка:", err)
		return 1
	}
	fmt.Fprintln(stderr, "ошибка: не удалось записать keys.json")
	return 1
}

// readKey читает одну строку. Если stdin — терминал, печатает приглашение в
// stderr и гасит эхо на время ввода.
func readKey(stdin io.Reader, stderr io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok {
		restore, interactive := muteEcho(f, stderr)
		if interactive {
			defer restore()
			fmt.Fprint(stderr, "Ключ API (ввод не отображается): ")
			defer fmt.Fprintln(stderr) // эхо Enter погашено: без этого следующая строка слипнется с приглашением
		}
	}
	line, err := bufio.NewReader(io.LimitReader(stdin, maxKeyLine)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// muteEcho выключает эхо терминала f командой stty (зависимость от внешней
// программы вместо зависимости в go.mod). interactive = true, если f —
// терминал; restore возвращает прежние настройки и безопасен к повторному
// вызову. На SIGINT и SIGTERM эхо возвращается до выхода: без этого после
// Ctrl-C терминал остался бы немым.
//
// Нет stty — не отказ: ввод продолжается с предупреждением, что он виден.
func muteEcho(f *os.File, stderr io.Writer) (func(), bool) {
	nop := func() {}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nop, false
	}
	stty := func(arg ...string) ([]byte, error) {
		cmd := exec.Command("stty", arg...)
		cmd.Stdin = f
		return cmd.Output()
	}
	saved, err := stty("-g") // удаётся только на настоящем терминале (не /dev/null)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			fmt.Fprintln(stderr, "предупреждение: программы stty нет — вводимый ключ будет виден на экране")
			return nop, true
		}
		return nop, false
	}
	if _, err := stty("-echo"); err != nil {
		fmt.Fprintln(stderr, "предупреждение: эхо терминала выключить не удалось — вводимый ключ будет виден на экране")
		return nop, true
	}
	var once sync.Once
	undo := func() {
		once.Do(func() { stty(strings.TrimSpace(string(saved))) })
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			undo()
			fmt.Fprintln(stderr)
			os.Exit(1)
		case <-done:
		}
	}()
	return func() { signal.Stop(sig); close(done); undo() }, true
}
