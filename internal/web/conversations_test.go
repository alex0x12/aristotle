package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
)

func writeJournal(t *testing.T, dir, id string, recs ...chat.Record) {
	t.Helper()
	h := chat.History{Path: filepath.Join(dir, id+".jsonl")}
	for _, r := range recs {
		if err := h.Append(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListSortedByUpdated(t *testing.T) {
	s := newTestServer(t, nil, nil)
	t0 := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	writeJournal(t, s.Paths.History, "2026-11-18T10-00-00",
		chat.Record{Time: t0, Role: "user", Text: "Что такое душа?"},
		chat.Record{Time: t0.Add(time.Minute), Role: "assistant", Text: "ответ"})
	writeJournal(t, s.Paths.History, "2026-11-17T09-00-00",
		chat.Record{Time: t0.Add(-24 * time.Hour), Role: "user", Text: "старый"},
		chat.Record{Time: t0.Add(time.Hour), Role: chat.RoleMeta, Title: "Переименована"})
	rec := do(t, s.Handler(), http.MethodGet, "/api/conversations", "")
	var list []convSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("список %s, %v", rec.Body.String(), err)
	}
	if list[0].Title != "Переименована" || list[1].Title != "Что такое душа?" {
		t.Fatalf("порядок или заголовки: %+v", list)
	}
}

func TestBadIDRejected(t *testing.T) {
	h := newTestServer(t, nil, nil).Handler()
	if rec := do(t, h, http.MethodGet, "/api/conversations/not-an-id", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("код %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/api/conversations/..%2F..%2Fsecret", ""); rec.Code == http.StatusOK {
		t.Fatal("выход за каталог журналов не отвергнут")
	}
}

func TestRenameAndDeleteMovesToTrash(t *testing.T) {
	s := newTestServer(t, nil, nil)
	h := s.Handler()
	writeJournal(t, s.Paths.History, convID, chat.Record{Role: "user", Text: "вопрос"})
	if rec := do(t, h, http.MethodPatch, "/api/conversations/"+convID, `{"title":"Новое имя"}`); rec.Code != 200 {
		t.Fatalf("переименование: %d", rec.Code)
	}
	recs, _ := chat.History{Path: filepath.Join(s.Paths.History, convID+".jsonl")}.Load()
	if chat.TitleOf(recs) != "Новое имя" {
		t.Fatalf("заголовок %q", chat.TitleOf(recs))
	}
	if rec := do(t, h, http.MethodDelete, "/api/conversations/"+convID, ""); rec.Code != 200 {
		t.Fatalf("удаление: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(s.Paths.History, "trash", convID+".jsonl")); err != nil {
		t.Fatalf("в корзине нет файла: %v", err)
	}
}

func TestOldAnswerWithoutQuotesIsMarkedUnknown(t *testing.T) {
	its := items([]chat.Record{
		{Role: "user", Text: "вопрос"},
		{Role: "assistant", Text: ">> cite: X 1\n>> en: words\n>> ru: слова\n"},
	})
	if len(its) != 2 || !its[1].QuotesUnknown || its[1].Blocks[0].Kind != "quote" {
		t.Fatalf("ответ из журнала без сверки: %+v", its)
	}
}
