package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	Time time.Time `json:"time"`
	Role string    `json:"role"`
	Text string    `json:"text"`
}

// History — журнал беседы в JSONL. Формат выбран ради дописывания без
// перечитывания файла.
type History struct {
	Path string
}

func (h History) Append(rec Record) error {
	if rec.Time.IsZero() {
		rec.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func (h History) Load() ([]Record, error) {
	f, err := os.Open(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue // битая строка не должна ронять всю беседу
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}
