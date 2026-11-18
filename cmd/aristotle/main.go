package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/render"
)

func main() {
	root := flag.String("root", ".", "корень репозитория (persona/ и corpus/)")
	modelName := flag.String("model", "", "профиль провайдера")
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
	cfg, err := config.Load(paths.Config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфиг:", err)
		os.Exit(1)
	}
	profile, err := cfg.Resolve(*modelName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	prov, err := app.NewProvider(profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
		OnSearchWarning: func(msg string) {
			fmt.Fprintln(os.Stderr, "предупреждение:", msg)
		},
		// Холодный qmd поднимается 8-13 секунд, и до четырёх обращений за
		// ход — молчание всё это время неотличимо от зависания.
		OnSearchStart: func(query, group string) {
			fmt.Fprintf(os.Stderr, "[ищу в корпусе: %s]\n", query)
		},
	}

	if *evalPath != "" {
		os.Exit(runEval(*evalPath, session, searcher, *recallOnly))
	}
	printer := &render.Printer{Out: os.Stdout, Color: true}

	fmt.Printf("Аристотель. Профиль %s (%s). /src, /model, /again, /quit.\n",
		profile.Name, profile.Model)
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

		case "/model":
			if len(fields) < 2 {
				fmt.Printf("текущий профиль: %s (%s)\n", profile.Name, profile.Model)
				fmt.Println("доступные профили:", strings.Join(app.ProfileNames(cfg), ", "))
				continue
			}
			newProfile, newProv, err := app.SwitchProfile(cfg, fields[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "ошибка:", err)
				continue
			}
			profile, prov = newProfile, newProv
			session.Provider = prov
			fmt.Printf("профиль переключён: %s (%s)\n", profile.Name, profile.Model)
			continue

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
		res, err := session.Turn(context.Background(), line,
			func(delta string) {
				for _, ev := range parser.Write(delta) {
					printer.Handle(ev, chapters)
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
		}
		appendHistory(chat.Record{Role: "assistant", Text: res.Text})
	}
}
