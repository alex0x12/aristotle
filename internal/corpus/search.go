package corpus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Hit struct {
	Chapter Chapter
	Score   float64
}

type qmdResult struct {
	File  string  `json:"file"`
	Score float64 `json:"score"`
}

// Searcher — обёртка над qmd. Run вынесен полем, чтобы тесты не запускали
// внешний процесс.
type Searcher struct {
	Collection string
	Root       string
	Run        func(ctx context.Context, args ...string) ([]byte, error)
}

func RunQmd(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "qmd", args...).Output()
}

// Search спрашивает qmd, дочитывает главы с диска и отсеивает неподлинное.
// qmd get не используется: он нумерует строки и игнорирует --json,
// а нумерация сломала бы сверку выдержек.
func (s *Searcher) Search(ctx context.Context, query, group string, k int) ([]Hit, error) {
	if k <= 0 {
		k = 6
	}
	run := s.Run
	if run == nil {
		run = RunQmd
	}

	out, err := run(ctx, "query", query, "-c", s.Collection,
		"--json", "-n", strconv.Itoa(k))
	if err != nil {
		return nil, fmt.Errorf("qmd query: %w", err)
	}

	var results []qmdResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Errorf("разбор выдачи qmd: %w", err)
	}

	prefix := "qmd://" + s.Collection + "/"
	hits := make([]Hit, 0, len(results))
	read := 0
	parsed := 0
	for _, r := range results {
		rel := strings.TrimPrefix(r.File, prefix)

		joined := filepath.Join(s.Root, rel)
		safe, err := filepath.Rel(s.Root, joined)
		if err != nil || safe == ".." || strings.HasPrefix(safe, ".."+string(filepath.Separator)) {
			continue // выдача qmd пытается увести чтение за пределы корня корпуса
		}

		data, err := os.ReadFile(joined)
		if err != nil {
			continue // индекс отстал от диска — не роняем беседу
		}
		read++

		ch, err := ParseChapter(rel, data)
		if err != nil {
			continue // главы читаются, но эта не разбирается — не роняем беседу целиком
		}
		parsed++

		if ch.Authenticity == "spurious" {
			continue
		}
		if group != "" && ch.Group != group {
			continue
		}
		hits = append(hits, Hit{Chapter: ch, Score: r.Score})
	}
	// Разделяем два разных отказа от честной пустоты. Оба счётчика растут до
	// фильтров spurious/group, поэтому выдача, целиком отсеянная фильтром —
	// все главы подлинные и разобранные, просто не той группы, — законная
	// пустота и сюда не попадает.
	switch {
	case len(results) > 0 && read == 0:
		// Ни одна глава не прочиталась с диска — почти наверняка неверно
		// указан корень корпуса.
		return nil, fmt.Errorf(
			"qmd вернул %d попаданий, но ни одна глава не прочиталась с диска; "+
				"похоже, неверный корень корпуса: %s", len(results), s.Root)
	case read > 0 && parsed == 0:
		// Главы читаются, но ни одна не разбирается — похоже, корпус собран
		// не тем конвейером, или его схема разошлась с ожидаемой.
		return nil, fmt.Errorf(
			"qmd вернул %d попаданий, главы читаются, но ни одна не разобралась; "+
				"похоже, корпус собран не тем конвейером или разошёлся со схемой", len(results))
	}
	// Пропуск отдельных нечитаемых или неразбираемых файлов при том, что
	// другие прочлись и разобрались, остаётся молчаливым: индекс мог просто
	// отстать от диска. Выдача, целиком отсеянная фильтрами spurious/group,
	// — тоже законная пустота, а не сбой.
	return hits, nil
}
