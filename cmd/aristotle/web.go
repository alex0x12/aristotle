package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/web"
)

// checkAddr принимает только адрес на петле: ключи управляются из
// интерфейса, и наружу его выставлять нельзя.
func checkAddr(addr string) (int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("адрес %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("адрес %q: порт должен быть числом от 1 до 65535", addr)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return 0, fmt.Errorf("адрес %q: слушать можно только 127.0.0.1, ::1 или localhost", addr)
		}
	}
	return port, nil
}

func runWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8337", "адрес: только 127.0.0.1, ::1 или localhost")
	noOpen := fs.Bool("no-open", false, "не открывать браузер")
	root := fs.String("root", ".", "корень репозитория (persona/ и corpus/)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	port, err := checkAddr(*addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// Без персоны беседы не будет — лучше узнать об этом при запуске.
	if _, err := persona.Load(filepath.Join(*root, "persona")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	srv := &http.Server{
		Handler: web.New(*root, paths, port).Handler(),
		// Без WriteTimeout: ответ философа идёт потоком минутами.
		ReadHeaderTimeout: 10 * time.Second,
	}
	url := "http://" + *addr + "/"
	fmt.Println("Аристотель:", url, "— Ctrl-C, чтобы остановить")
	if !*noOpen {
		openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case <-ctx.Done():
	}
	// Shutdown ждёт идущие ходы, чтобы журнал дописался; поток ответа держит
	// соединение, поэтому ждём не дольше пяти секунд.
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
	return 0
}

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "браузер не открылся:", err)
		return
	}
	go cmd.Wait() // не оставлять зомби
}
