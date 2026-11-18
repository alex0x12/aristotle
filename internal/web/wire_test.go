package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

// Проводная форма, на которую опирается интерфейс. Поля с omitempty на проводе
// бывают без ключа; у пустой строки ответа (разрыв абзаца) блок prose приходит
// без text, и в журнале-ответе беседы тоже. Интерфейс читает такие поля
// терпимо (b.text ?? ''), а тесты закрепляют саму форму: тег render.Block —
// контракт задачи 5, и его смена должна быть заметна здесь, а не в браузере.

func TestBlankProseBlockHasNoTextKey(t *testing.T) {
	raw, err := json.Marshal(render.Block{Kind: render.BlockProse})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, has := got["text"]; has {
		t.Errorf("у пустого блока prose есть ключ text: %s", raw)
	}
	if got["kind"] != "prose" || got["verified"] != false {
		t.Errorf("kind и verified всегда на проводе, получено %s", raw)
	}
}

// Настоящий ход с пустой строкой в ответе: блок разрыва абзаца уходит потоком
// и лежит в беседе после перезагрузки, оба раза без text.
func TestBlankLineInAnswerIsBlockWithoutText(t *testing.T) {
	text := func(s string) provider.Event { return provider.Event{Kind: provider.EventText, Text: s} }
	p := &scriptedProvider{turns: [][]provider.Event{{
		text("Первый абзац.\n"), text("\n"), text("Второй абзац.\n"),
		{Kind: provider.EventUsage, Usage: provider.Usage{Input: 10, Output: 5, Known: true}},
		{Kind: provider.EventDone, StopReason: "end_turn"},
	}}}
	s := newTestServer(t, p, fakeFinder{})
	h := s.Handler()

	rec := do(t, h, http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var blocks []map[string]any
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "block" {
			blocks = append(blocks, ev.Data)
		}
	}
	checkProse := func(where string, blocks []map[string]any) {
		t.Helper()
		if len(blocks) != 3 {
			t.Fatalf("%s: блоков %d (%v), ожидалось 3", where, len(blocks), blocks)
		}
		if blocks[0]["text"] != "Первый абзац." || blocks[2]["text"] != "Второй абзац." {
			t.Errorf("%s: текст абзацев %v / %v", where, blocks[0], blocks[2])
		}
		blank := blocks[1]
		if _, has := blank["text"]; has {
			t.Errorf("%s: у блока пустой строки есть ключ text: %v", where, blank)
		}
		if blank["kind"] != "prose" || blank["verified"] != false {
			t.Errorf("%s: блок пустой строки %v", where, blank)
		}
	}
	checkProse("поток", blocks)

	view := do(t, h, http.MethodGet, "/api/conversations/"+convID, "")
	var conv struct {
		Items []struct {
			Type   string           `json:"type"`
			Blocks []map[string]any `json:"blocks"`
		} `json:"items"`
	}
	if err := json.Unmarshal(view.Body.Bytes(), &conv); err != nil {
		t.Fatal(err)
	}
	if len(conv.Items) != 2 || conv.Items[1].Type != "assistant" {
		t.Fatalf("беседа %s", view.Body.String())
	}
	checkProse("беседа", conv.Items[1].Blocks)
}
