package web

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
)

type usageRow struct {
	Day        string `json:"day,omitempty"`
	Profile    string `json:"profile,omitempty"`
	Input      int    `json:"input"`
	Output     int    `json:"output"`
	CacheRead  int    `json:"cache_read"`
	CacheWrite int    `json:"cache_write"`
	// CostUSD — сумма цен ответов; нет ни одной цены — поля нет вовсе, и
	// «$» не показывается. Ноль долларов и «цены неизвестны» — разные вещи.
	CostUSD *float64 `json:"cost_usd,omitempty"`
	// CostPartial — у части ответов цены нет: сумма занижена.
	CostPartial bool `json:"cost_partial"`
	// Partial — в строке есть ответы с known:false (оборванный ход, провайдер
	// не назвал токенов): числа в ней — то, что удалось сосчитать, а не итог.
	Partial bool `json:"partial"`
	Answers int  `json:"answers"`
}

// add прибавляет запись журнала; usage у неё уже проверен на nil.
func (r *usageRow) add(rec chat.Record) {
	u := rec.Usage
	r.Input += u.Input
	r.Output += u.Output
	r.CacheRead += u.CacheRead
	r.CacheWrite += u.CacheWrite
	r.Answers++
	if !u.Known {
		r.Partial = true
	}
	if rec.CostUSD != nil {
		sum := *rec.CostUSD
		if r.CostUSD != nil {
			sum += *r.CostUSD
		}
		r.CostUSD = &sum
	} else {
		r.CostPartial = true
	}
}

// aggregate — расход по дням и профилям из журналов каталога dir и его
// подкаталога trash/ за [from, to] включительно, по местным датам. Отозванные
// «ещё раз» ответы тоже считаются: деньги за них потрачены. Ответы с known:false
// (оборванные, неудавшиеся) тоже входят — своими частичными числами, но строка
// с ними помечена Partial. Записи без usage (журналы до появления учёта) не
// считаются. Корзина считается наравне с остальным: удаление беседы не должно
// уменьшать итог. Других подкаталогов и вложенных в корзину aggregate не читает.
//
// Журнал, который не прочитался (строка длиннее предела History.Load, ошибка
// чтения), не исчезает молча: строка о нём — в журнал сервера (без содержимого),
// а итог помечается Partial, если журнал мог иметь ответы в периоде. К какому
// дню и профилю относится непрочитанное, неизвестно, поэтому строки не метятся.
func aggregate(dir string, from, to time.Time) ([]usageRow, usageRow, error) {
	var total usageRow
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []usageRow{}, total, nil
	}
	if err != nil {
		return nil, total, err
	}
	lo, hi := from.Format("2006-01-02"), to.Format("2006-01-02")
	rows := map[[2]string]*usageRow{}
	read := func(d string, entries []os.DirEntry) {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			recs, err := chat.History{Path: filepath.Join(d, e.Name())}.Load()
			if err != nil {
				// Load отдаёт и прочитанное до сбоя — оно считается.
				log.Printf("журнал %s не прочитан целиком: %v", e.Name(), err)
				if mayHaveAnswersBy(e.Name(), to) {
					total.Partial = true
				}
			}
			for _, rec := range recs {
				if rec.Role != "assistant" || rec.Usage == nil {
					continue
				}
				day := rec.Time.Local().Format("2006-01-02")
				if day < lo || day > hi {
					continue
				}
				key := [2]string{day, rec.Profile}
				if rows[key] == nil {
					rows[key] = &usageRow{Day: day, Profile: rec.Profile}
				}
				rows[key].add(rec)
				total.add(rec)
			}
		}
	}
	read(dir, entries)
	trash := filepath.Join(dir, "trash")
	if te, err := os.ReadDir(trash); err == nil {
		read(trash, te)
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("корзина журналов не прочитана: %v", err)
	}
	out := make([]usageRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day > out[j].Day
		}
		return out[i].Profile < out[j].Profile
	})
	return out, total, nil
}

// mayHaveAnswersBy — мог ли журнал с таким именем иметь ответы не позже дня to.
// Имя — время начала беседы в UTC (app.HistoryLayout): начатая позже конца
// периода (с запасом на сдвиг часового пояса) ответов в нём не имеет. Имя не
// по образцу — считаем, что мог.
func mayHaveAnswersBy(name string, to time.Time) bool {
	if len(name) < len("2006-01-02") {
		return true
	}
	start, err := time.Parse("2006-01-02", name[:len("2006-01-02")])
	if err != nil {
		return true
	}
	return !start.After(to.AddDate(0, 0, 2))
}

// monthBounds — первый и последний день месяца now по местному времени.
func monthBounds(now time.Time) (time.Time, time.Time) {
	now = now.In(time.Local)
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	return first, first.AddDate(0, 1, -1)
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	from, to := monthBounds(time.Now())
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &from}, {"to", &to}} {
		v := r.URL.Query().Get(p.name)
		if v == "" {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		if err != nil {
			http.Error(w, p.name+": ожидается ГГГГ-ММ-ДД", http.StatusBadRequest)
			return
		}
		*p.dst = t
	}
	if from.After(to) {
		http.Error(w, "from позже to", http.StatusBadRequest)
		return
	}
	rows, total, err := aggregate(s.Paths.History, from, to)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"rows": rows, "total": total,
		"from": from.Format("2006-01-02"), "to": to.Format("2006-01-02")})
}
