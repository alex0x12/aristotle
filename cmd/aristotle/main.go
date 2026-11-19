package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

const usage = `использование:
  aristotle [флаги]       беседа в терминале
  aristotle web [флаги]   веб-интерфейс (aristotle web -h)
  aristotle key           сохранить ключ API (aristotle key -h)

Модель одна: адрес API, название модели и бюджет контекста — в
~/.config/aristotle/config.json или в «Настройках» веб-интерфейса, ключ — в
~/.config/aristotle/keys.json (команда aristotle key). Переменные окружения
для ключа не читаются.

флаги беседы:
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "web":
			os.Exit(runWeb(os.Args[2:]))
		case "key":
			os.Exit(runKeyCLI(os.Args[2:]))
		}
	}
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	root := flag.String("root", ".", "корень репозитория (persona/ и corpus/)")
	resume := flag.String("resume", "", "продолжить беседу из файла истории")
	evalPath := flag.String("eval", "", "прогнать вопросы из файла и выйти")
	recallOnly := flag.Bool("recall-only", false,
		"только recall, без обращений к модели")
	flag.Parse()

	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn, cfgErr, keyErr := app.LoadConnection(paths)
	if cfgErr != nil {
		// Не отказ: беседа идёт на умолчаниях, но молча подменять настройки нельзя.
		fmt.Fprintf(os.Stderr, "предупреждение: config.json не читается: %v — беру умолчания\n", cfgErr)
	}
	// recall-only модель не зовёт, и ключ ему не нужен: он работает и без
	// ключа, и с испорченным keys.json.
	needModel := !(*evalPath != "" && *recallOnly)
	var prov provider.Provider
	if needModel {
		if keyErr != nil {
			var ue *keys.UnreadableError
			if errors.As(keyErr, &ue) {
				fmt.Fprintf(os.Stderr, "%s (%s): исправьте файл или удалите его вручную\n", keys.UnreadableText, paths.Keys)
			} else {
				fmt.Fprintln(os.Stderr, keyErr)
			}
			os.Exit(1)
		}
		if prov, err = app.NewProvider(conn); err != nil { // нет ключа — app.ErrNoKey
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	system, err := persona.Load(filepath.Join(*root, "persona"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	historyPath := *resume
	if historyPath == "" {
		historyPath = app.NewHistoryPath(paths.History, time.Now())
	}
	history := chat.History{Path: historyPath}

	searcher := app.NewSearcher(*root)
	session := &chat.Session{Provider: prov, Corpus: searcher, System: system,
		Budget: conn.ContextBudget,
		OnSearchWarning: func(msg string) {
			fmt.Fprintln(os.Stderr, "предупреждение:", msg)
		},
		// Холодный qmd поднимается 8-13 секунд, и до четырёх обращений за
		// ход — молчание всё это время неотличимо от зависания.
		OnSearchStart: func(query, group string) {
			fmt.Fprintf(os.Stderr, "[ищу в корпусе: %s]\n", query)
		},
		// В режиме размышления до первого слова проходит до минуты.
		OnModelStart: func() {
			fmt.Fprintln(os.Stderr, "[размышляю]")
		},
		OnNotice: func(text string) {
			fmt.Fprintln(os.Stderr, "предупреждение:", text)
		},
	}

	if *evalPath != "" {
		os.Exit(runEval(*evalPath, session, searcher, *recallOnly))
	}
	printer := &render.Printer{Out: os.Stdout, Color: true}

	fmt.Printf("Аристотель. Модель %s (%s). /src, /again, /quit.\n", conn.Model, hostOf(conn.BaseURL))
	fmt.Println("История:", historyPath)

	if *resume != "" {
		records, err := history.Load()
		switch {
		case err != nil:
			fmt.Fprintln(os.Stderr, "история повреждена, начинаю новую беседу:", err)
		case len(records) == 0:
			fmt.Fprintln(os.Stderr, "история пуста или не найдена, начинаю новую беседу")
		default:
			session.Messages = app.ResumeMessages(records)
			fmt.Printf("возобновлено реплик: %d\n", len(session.Messages))
		}
	}

	// historyWarned — предупреждение о неработающем журнале печатается один
	// раз за сессию, а не на каждой реплике: если ~/.local/share недоступен,
	// это одна и та же причина всякий раз.
	var historyWarned bool
	appendHistory := func(rec chat.Record) {
		if err := history.Append(rec); err != nil && !historyWarned {
			fmt.Fprintln(os.Stderr, "предупреждение: журнал беседы не записывается:", err)
			historyWarned = true
		}
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("\n> ")
		if !in.Scan() {
			return
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		switch fields[0] {
		case "/quit":
			return

		case "/src":
			src := printer.Sources()
			if len(src) == 0 {
				fmt.Println("выдержек в последнем ответе не было")
				continue
			}
			for _, s := range src {
				fmt.Println(" ", s)
			}
			continue

		case "/again":
			last := app.LastUserMessage(session.Messages)
			if last == "" {
				fmt.Println("истории нет, нечего повторять")
				continue
			}
			line = last
		}

		appendHistory(chat.Record{Role: "user", Text: line})
		printer.Reset()

		// chapters — главы, накопленные к текущему моменту хода. onChapters
		// обновляет их сразу после каждого обращения к поиску — раньше, чем
		// модель напишет первую выдержку, — поэтому к моменту, когда parser
		// разберёт строку с врезкой, сверка уже видит нужные главы, а не
		// пустой список.
		var chapters []corpus.Chapter
		parser := &render.Parser{}
		// asm собирает те же события в блоки: результат сверки зависит от
		// глав этого хода, поэтому врезки уходят в журнал сразу вместе с
		// ответом.
		asm := &render.Assembler{}
		var blocks []render.Block
		res, err := session.Turn(context.Background(), line,
			func(delta string) {
				for _, ev := range parser.Write(delta) {
					printer.Handle(ev, chapters)
					blocks = append(blocks, asm.Handle(ev, chapters)...)
				}
			},
			func(cs []corpus.Chapter) {
				chapters = cs
			},
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nошибка:", err)
			continue
		}
		for _, ev := range parser.Flush() {
			printer.Handle(ev, chapters)
			blocks = append(blocks, asm.Handle(ev, chapters)...)
		}
		blocks = append(blocks, asm.Flush()...)
		if res.Trim.Any() {
			appendHistory(chat.Record{Role: chat.RoleTrim,
				Excerpts: res.Trim.Excerpts, Turns: res.Trim.Turns})
		}
		usage := res.Usage
		appendHistory(chat.Record{Role: "assistant", Text: res.Text,
			Reasoning: res.Reasoning, Model: conn.Model, Usage: &usage,
			Quotes: app.QuotesOf(blocks), StopReason: "end"})
		fmt.Fprintln(os.Stderr, app.UsageLine(res.Usage))
		if res.Trim.Any() {
			fmt.Fprintf(os.Stderr, "[контекст: опущено выдержек %d, ходов %d]\n",
				res.Trim.Excerpts, res.Trim.Turns)
		}
		if res.OverBudget {
			fmt.Fprintln(os.Stderr, "[контекст: бюджет превышен и после урезания — запрос отправлен в урезанном виде]")
		}
	}
}

// hostOf — хост адреса API для баннера: путь и параметры адреса показывать
// незачем.
func hostOf(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return baseURL
}
