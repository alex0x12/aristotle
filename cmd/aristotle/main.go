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
	session := &chat.Session{Provider: prov, Corpus: searcher, System: system}

	if *evalPath != "" {
		os.Exit(runEval(*evalPath, session, searcher, *recallOnly))
	}
	printer := &render.Printer{Out: os.Stdout, Color: true}

	fmt.Printf("Аристотель. Профиль %s (%s). /src, /model, /quit.\n",
		profile.Name, profile.Model)
	fmt.Println("История:", historyPath)

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
		switch {
		case line == "/quit":
			return
		case line == "/model":
			fmt.Printf("%s (%s)\n", profile.Name, profile.Model)
			continue
		case line == "/src":
			src := printer.Sources()
			if len(src) == 0 {
				fmt.Println("выдержек в последнем ответе не было")
				continue
			}
			for _, s := range src {
				fmt.Println(" ", s)
			}
			continue
		}

		history.Append(chat.Record{Role: "user", Text: line})
		printer.Reset()

		parser := &render.Parser{}
		var events []render.Event
		res, err := session.Turn(context.Background(), line, func(delta string) {
			events = append(events, parser.Write(delta)...)
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "\nошибка:", err)
			continue
		}
		events = append(events, parser.Flush()...)
		for _, ev := range events {
			printer.Handle(ev, res.Chapters)
		}
		history.Append(chat.Record{Role: "assistant", Text: res.Text})
	}
}
