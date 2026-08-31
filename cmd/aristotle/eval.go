// cmd/aristotle/eval.go
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/eval"
	"github.com/alex0x12/aristotle/internal/render"
)

func runEval(path string, session *chat.Session, searcher *corpus.Searcher,
	recallOnly bool) int {

	questions, err := eval.LoadQuestions(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "вопросы:", err)
		return 1
	}

	ctx := context.Background()
	var report eval.Report
	var durations []time.Duration

	fmt.Println("RECALL")
	for _, q := range questions {
		if q.Expect == "" {
			continue // ловушка: адреса нет, оценивается поведение
		}
		report.Total++
		start := time.Now()
		hits, err := searcher.Search(ctx, q.Ask, q.Group, 6)
		durations = append(durations, time.Since(start))
		if err != nil {
			fmt.Printf("  ОШИБКА  %s: %v\n", q.Ask, err)
			continue
		}
		if eval.RecallAt(hits, q.Expect) {
			report.Found++
			fmt.Printf("  есть    %s\n", q.Expect)
			continue
		}
		fmt.Printf("  НЕТ     %s  (вопрос: %s)\n", q.Expect, q.Ask)
		for _, h := range hits {
			fmt.Printf("            вернулось: %s\n", h.Chapter.Path)
		}
	}
	report.Finish()

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	fmt.Printf("\nrecall@6 при наивном запросе, одной попыткой: %d/%d (%.0f%%)\n",
		report.Found, report.Total, report.Recall*100)
	fmt.Println("это нижняя граница, не оценка системы: вопрос из questions.yaml " +
		"подаётся в поиск дословно, а агент в работе переформулирует запрос в " +
		"понятиях корпуса и ищет до четырёх раз за ход")
	if n := len(durations); n > 0 {
		fmt.Printf("медиана поиска: %s, худший: %s\n",
			durations[n/2].Round(time.Millisecond),
			durations[n-1].Round(time.Millisecond))
	}
	if recallOnly {
		return 0
	}

	fmt.Println("\nВЕРНОСТЬ И ЛОВУШКИ")
	for _, q := range questions {
		// Свежая сессия на каждый вопрос: иначе предыдущие ответы
		// протекают в контекст и портят замер.
		fresh := &chat.Session{
			Provider: session.Provider,
			Corpus:   session.Corpus,
			System:   session.System,
		}
		parser := &render.Parser{}
		var events []render.Event
		res, err := fresh.Turn(ctx, q.Ask, func(delta string) {
			events = append(events, parser.Write(delta)...)
		})
		if err != nil {
			fmt.Printf("\n[%s]\n  ОШИБКА: %v\n", q.Ask, err)
			continue
		}
		events = append(events, parser.Flush()...)

		for _, ev := range events {
			if ev.Kind != render.En {
				continue
			}
			report.Quotes++
			if !render.Verify(ev.Text, res.Chapters) {
				report.Fabricated++
				fmt.Printf("  ВЫДУМКА (%s): %s\n", q.Ask, ev.Text)
			}
		}
		if q.Note != "" {
			fmt.Printf("\n[%s]\n  ожидание: %s\n  ответ: %s\n",
				q.Ask, q.Note, res.Text)
		}
	}

	fmt.Printf("\nвыдуманных выдержек: %d из %d\n",
		report.Fabricated, report.Quotes)
	if report.Fabricated > 0 {
		return 1 // ненулевой код: прогон годится для CI
	}
	return 0
}
