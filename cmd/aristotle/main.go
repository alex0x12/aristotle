package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/persona"
	"github.com/alex0x12/aristotle/internal/provider"
	anth "github.com/alex0x12/aristotle/internal/provider/anthropic"
	"github.com/alex0x12/aristotle/internal/provider/openaicompat"
	"github.com/alex0x12/aristotle/internal/render"
)

func newProvider(p config.Profile) (provider.Provider, error) {
	switch p.Kind {
	case "anthropic":
		return anth.New(p)
	case "openai":
		return openaicompat.New(p)
	default:
		return nil, fmt.Errorf("профиль %s: неизвестный kind %q", p.Name, p.Kind)
	}
}

// switchProfile разрешает имя профиля и создаёт для него провайдера. Не
// трогает ничего снаружи: при ошибке (нет такого профиля, не задан ключ)
// вызывающая сторона просто не подставляет возвращённые значения, и прежний
// профиль остаётся рабочим.
func switchProfile(cfg config.Config, name string) (config.Profile, provider.Provider, error) {
	p, err := cfg.Resolve(name)
	if err != nil {
		return config.Profile{}, nil, err
	}
	prov, err := newProvider(p)
	if err != nil {
		return config.Profile{}, nil, err
	}
	return p, prov, nil
}

// profileNames — отсортированный список имён профилей, для подсказки в /model.
func profileNames(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resumeMessages превращает записи истории в сообщения провайдера. Роли,
// которых чат не понимает (не "user" и не "assistant" — например, битая
// запись с пустой ролью), пропускаются, а не роняют восстановление целиком.
// Запись ассистента с пустым текстом тоже пропускается: это след оборванного
// хода (ошибка провайдера, Ctrl-C посреди ответа), а не настоящая реплика —
// сообщение без содержимого провайдер не примет.
//
// История обрывается репликой пользователя после ЛЮБОЙ ошибки провайдера
// или Ctrl-C — то есть как раз в главном случае, ради которого возобновляют:
// вопрос записан, ответ — нет. Хвостовые записи с ролью user, оставшиеся без
// ответа ассистента, поэтому отбрасываются: иначе следующий ход дописывает
// вторую подряд реплику user, а API такое отвергает.
func resumeMessages(records []chat.Record) []provider.Message {
	msgs := make([]provider.Message, 0, len(records))
	for _, rec := range records {
		var role provider.Role
		switch rec.Role {
		case "user":
			role = provider.RoleUser
		case "assistant":
			if rec.Text == "" {
				continue
			}
			role = provider.RoleAssistant
		default:
			continue
		}
		msgs = append(msgs, provider.Message{Role: role, Text: rec.Text})
	}
	for len(msgs) > 0 && msgs[len(msgs)-1].Role == provider.RoleUser {
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

// lastUserMessage ищет последнюю реплику пользователя в истории сессии —
// для /again. Пустая строка означает, что повторять нечего.
//
// Результаты инструмента тоже несут Role: RoleUser (так chat.Session.Turn
// передаёт их провайдеру — см. internal/chat/loop.go) и идут в Messages
// вперемешку с настоящими репликами человека. У них Text всегда пуст и
// заполнен только ToolResults, поэтому такие записи обязаны пропускаться:
// иначе после хода с обращением к поиску /again находит пустую строку от
// последнего результата инструмента, а не настоящий последний вопрос.
func lastUserMessage(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == provider.RoleUser && len(m.ToolResults) == 0 {
			return m.Text
		}
	}
	return ""
}

func main() {
	root := flag.String("root", ".", "корень репозитория (persona/ и corpus/)")
	modelName := flag.String("model", "", "профиль провайдера")
	resume := flag.String("resume", "", "продолжить беседу из файла истории")
	evalPath := flag.String("eval", "", "прогнать вопросы из файла и выйти")
	recallOnly := flag.Bool("recall-only", false,
		"только recall, без обращений к модели")
	flag.Parse()

	home, _ := os.UserHomeDir()
	cfg, err := config.Load(filepath.Join(home, ".config", "aristotle", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфиг:", err)
		os.Exit(1)
	}
	profile, err := cfg.Resolve(*modelName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	prov, err := newProvider(profile)
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
		dir := filepath.Join(home, ".local", "share", "aristotle")
		historyPath = filepath.Join(dir,
			time.Now().UTC().Format("2006-01-02T15-04-05")+".jsonl")
	}
	history := chat.History{Path: historyPath}

	searcher := &corpus.Searcher{
		Collection: "aristotle",
		Root:       filepath.Join(*root, "corpus"),
	}
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
			session.Messages = resumeMessages(records)
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
				fmt.Println("доступные профили:", strings.Join(profileNames(cfg), ", "))
				continue
			}
			newProfile, newProv, err := switchProfile(cfg, fields[1])
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
			last := lastUserMessage(session.Messages)
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
