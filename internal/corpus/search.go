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
	"time"
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

// qmdWaitDelay — сколько ждать после отмены контекста, прежде чем оборвать
// каналы qmd силой. qmd — скрипт Node: по отмене убивается только он сам, а
// потомки (npm, модель) живут и держат конец канала stdout, и без этого
// предела Output() не вернулся бы, пока они не выйдут.
const qmdWaitDelay = 5 * time.Second

func RunQmd(ctx context.Context, args ...string) ([]byte, error) {
	return runCommand(ctx, "qmd", qmdWaitDelay, args...)
}

func runCommand(ctx context.Context, name string, waitDelay time.Duration, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	return cmd.Output()
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

	// Запрос — позиционный аргумент, а qmd читает начало с «-» как свой флаг
	// («--help» печатает справку, «-c чужая» подменяет коллекцию). Поэтому
	// флаги идут первыми, а запрос — последним, после «--»: всё за ним qmd
	// (util.parseArgs) принимает как позиционное, что бы там ни было написано.
	//
	// --no-rerank: реранкер qmd (Qwen3-0.6B) в свежем процессе инициализируется
	// десятками секунд, и на ноутбучном GPU это дороже всего остального поиска —
	// замер на 7 вопросах eval/questions.yaml дал 12 с медианы и 8 мин 55 с в
	// худшем случае с реранком против 3,2 с и 5,2 с без него, при recall 4/6
	// против 6/7. Ранжирование остаётся гибридным (lex+vec+hyde, слияние RRF),
	// теряется только LLM-переупорядочивание.
	out, err := run(ctx, "query", "-c", s.Collection,
		"--json", "-n", strconv.Itoa(k), "--no-rerank", "--", query)
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
