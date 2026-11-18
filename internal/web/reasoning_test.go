package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/app"
	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
)

// Рассуждение модели уходит в журнал (без него после перезапуска сервер откажет
// в запросе) и больше никуда: ни в поток событий, ни в беседу, ни в экспорт.
// Разовое предупреждение провайдера идёт обычным событием warning.
func TestReasoningIsJournaledButNeverShown(t *testing.T) {
	const secret = "СЕКРЕТНОЕ-РАССУЖДЕНИЕ-МОДЕЛИ"
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventNotice, Text: "разовое предупреждение провайдера"},
		{Kind: provider.EventReasoning, Text: secret},
		{Kind: provider.EventText, Text: "Ответ."},
		{Kind: provider.EventUsage, Usage: provider.Usage{Input: 100, Output: 10, Known: true}},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}, fakeFinder{hits: []corpus.Hit{}})
	h := s.Handler()

	rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("рассуждение попало в поток событий")
	}
	var warned bool
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "warning" && ev.Data["text"] == "разовое предупреждение провайдера" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("предупреждение провайдера не дошло событием warning: %s", rec.Body.String())
	}

	for _, path := range []string{"/api/conversations/" + convID, "/api/conversations/" + convID + "/export.md", "/api/conversations", "/api/usage"} {
		r := do(t, h, http.MethodGet, path, "")
		if strings.Contains(r.Body.String(), secret) {
			t.Fatalf("рассуждение попало в ответ %s", path)
		}
	}

	recs, err := chat.History{Path: s.conv(convID).history.Path}.Load()
	if err != nil || len(recs) != 2 || recs[1].Reasoning != secret {
		t.Fatalf("журнал: рассуждение не записано: %+v, %v", recs, err)
	}
	// После перезапуска история собирается из журнала — с рассуждением.
	msgs := app.ResumeMessages(recs)
	if len(msgs) != 2 || msgs[1].Reasoning != secret {
		t.Fatalf("восстановленная история без рассуждения: %+v", msgs)
	}
}
