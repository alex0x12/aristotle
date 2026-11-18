package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// Беседа и список отдаются без цены и профиля, даже если журнал прежней версии
// их хранит; модель и расход остаются.
func TestConversationJSONHasNoCostOrProfile(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeRawJournal(t, s.Paths.History, convID,
		`{"time":"2026-11-10T10:00:00Z","role":"user","text":"Что такое добродетель?"}`,
		`{"time":"2026-11-10T10:00:05Z","role":"assistant","text":"Надвое.\n","profile":"deepseek-pro","model":"deepseek-v4-pro","usage":{"input":2000,"output":300,"cache_read":16000,"cache_write":120,"known":true},"cost_usd":0.0031,"stop_reason":"end"}`)
	h := s.Handler()

	get := do(t, h, http.MethodGet, "/api/conversations/"+convID, "")
	list := do(t, h, http.MethodGet, "/api/conversations", "")
	if get.Code != 200 || list.Code != 200 {
		t.Fatalf("коды %d, %d", get.Code, list.Code)
	}
	for name, body := range map[string]string{"беседа": get.Body.String(), "список": list.Body.String()} {
		for _, gone := range []string{"cost", "profile", "0.0031"} {
			if strings.Contains(body, gone) {
				t.Errorf("%s: в ответе есть %q: %s", name, gone, body)
			}
		}
	}
	var conv struct {
		ID     string `json:"id"`
		Tokens int    `json:"tokens"`
		Items  []struct {
			Type  string         `json:"type"`
			Model string         `json:"model"`
			Usage map[string]any `json:"usage"`
		} `json:"items"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &conv); err != nil || len(conv.Items) != 2 {
		t.Fatalf("беседа %s, %v", get.Body.String(), err)
	}
	if a := conv.Items[1]; a.Model != "deepseek-v4-pro" || a.Usage["input"] != 2000.0 || a.Usage["cache_read"] != 16000.0 {
		t.Fatalf("ответ ассистента %+v", a)
	}
	var m []map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &m); err != nil || len(m) != 1 {
		t.Fatalf("список %s, %v", list.Body.String(), err)
	}
	if got := keysOfMap(m[0]); got != "id,title,tokens,updated" {
		t.Fatalf("поля элемента списка: %s", got)
	}
}
