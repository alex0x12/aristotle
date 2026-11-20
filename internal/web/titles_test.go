package web

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/corpus"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

// Русские названия на проводе — только для показа. Тесты закрепляют обе
// стороны: что поля приходят и что ничего из того, что уже было контрактом
// (render.Block, журнал, английский cite), от них не изменилось.

// streamBlocks гонит ход с готовым ответом модели и возвращает блоки потока.
func streamBlocks(t *testing.T, answerText string) (*Server, []map[string]any) {
	t.Helper()
	var evs []provider.Event
	for _, line := range strings.SplitAfter(answerText, "\n") {
		if line != "" {
			evs = append(evs, provider.Event{Kind: provider.EventText, Text: line})
		}
	}
	evs = append(evs,
		provider.Event{Kind: provider.EventUsage, Usage: provider.Usage{Input: 10, Output: 5, Known: true}},
		provider.Event{Kind: provider.EventDone, StopReason: "end_turn"})
	s := newTestServer(t, &scriptedProvider{turns: [][]provider.Event{evs}}, fakeFinder{})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"вопрос"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	var blocks []map[string]any
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name == "block" {
			blocks = append(blocks, ev.Data)
		}
	}
	return s, blocks
}

func quotesOnly(blocks []map[string]any) []map[string]any {
	var out []map[string]any
	for _, b := range blocks {
		if b["kind"] == "quote" {
			out = append(out, b)
		}
	}
	return out
}

func TestStreamedQuoteCarriesRussianCite(t *testing.T) {
	s := newTestServer(t, &scriptedProvider{turns: script()}, fakeFinder{hits: []corpus.Hit{{Chapter: ethics}}})
	rec := do(t, s.Handler(), http.MethodPost, "/api/conversations/"+convID+"/turns", `{"text":"Что такое добродетель?"}`)
	if rec.Code != 200 {
		t.Fatalf("код %d", rec.Code)
	}
	var quotes, others []map[string]any
	for _, ev := range parseSSE(t, rec.Body.String()) {
		if ev.Name != "block" {
			continue
		}
		if ev.Data["kind"] == "quote" {
			quotes = append(quotes, ev.Data)
		} else {
			others = append(others, ev.Data)
		}
	}
	if len(quotes) != 2 {
		t.Fatalf("врезок %d, ожидалось 2", len(quotes))
	}
	for i, q := range quotes {
		if q["cite_ru"] != "Никомахова этика II.1" || q["work_ru"] != "Никомахова этика" {
			t.Errorf("врезка %d: cite_ru=%v work_ru=%v", i, q["cite_ru"], q["work_ru"])
		}
		// Английская ссылка и всё прежнее на проводе остаются как были.
		if q["cite"] != "Nicomachean Ethics II.1" || q["en"] == nil || q["ru"] == nil || q["verified"] == nil {
			t.Errorf("врезка %d: прежние поля изменились: %v", i, q)
		}
	}
	if quotes[0]["verified"] != true || quotes[0]["chapter"] != ethics.Path {
		t.Errorf("сверка и путь главы: %v", quotes[0])
	}
	// Не врезкам русского названия не полагается — ни пустого, ни какого-либо.
	for _, b := range others {
		if _, has := b["cite_ru"]; has {
			t.Errorf("у блока %v есть cite_ru", b["kind"])
		}
		if _, has := b["work_ru"]; has {
			t.Errorf("у блока %v есть work_ru", b["kind"])
		}
	}

	// Журнал английский: русское название в него не пишется.
	raw, err := os.ReadFile(s.conv(convID).history.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cite_ru") || strings.Contains(string(raw), "work_ru") ||
		strings.Contains(string(raw), "Никомахова") {
		t.Fatalf("русское название попало в журнал: %s", raw)
	}
	recs, err := chat.History{Path: s.conv(convID).history.Path}.Load()
	if err != nil || len(recs) != 2 || len(recs[1].Quotes) != 2 || recs[1].Quotes[0].Cite != "Nicomachean Ethics II.1" {
		t.Fatalf("врезки в журнале: %+v, %v", recs, err)
	}
}

func TestStreamedUnknownCiteHasNoRussianKeys(t *testing.T) {
	_, blocks := streamBlocks(t, ""+
		">> cite: The Constitution of Fictionland 3\n"+
		">> en: no such work\n"+
		">> ru: нет такой работы\n"+
		">> cite: Nicomachean Ethics\n"+ // название без нумерации — не ссылка
		">> en: bare title\n"+
		">> ru: голое название\n"+
		">> en: a quote with no cite at all\n"+
		">> ru: врезка без ссылки\n")
	quotes := quotesOnly(blocks)
	if len(quotes) != 3 {
		t.Fatalf("врезок %d (%v), ожидалось 3", len(quotes), blocks)
	}
	for i, q := range quotes {
		if _, has := q["cite_ru"]; has {
			t.Errorf("врезка %d: ключ cite_ru есть, а перевести нечего (даже пустой): %v", i, q)
		}
		if _, has := q["work_ru"]; has {
			t.Errorf("врезка %d: ключ work_ru есть: %v", i, q)
		}
	}
	if quotes[0]["cite"] != "The Constitution of Fictionland 3" || quotes[1]["cite"] != "Nicomachean Ethics" {
		t.Errorf("английский cite изменён: %v / %v", quotes[0]["cite"], quotes[1]["cite"])
	}
}

// Врезка, оставшаяся без ru (модель оборвалась), уходит через Flush — тем же
// путём, и русское название у неё тоже есть.
func TestStreamedQuoteWithoutRuStillHasRussianCite(t *testing.T) {
	_, blocks := streamBlocks(t, ">> cite: Politics I.2\n>> en: man is by nature a political animal\n")
	quotes := quotesOnly(blocks)
	if len(quotes) != 1 || quotes[0]["cite_ru"] != "Политика I.2" || quotes[0]["work_ru"] != "Политика" {
		t.Fatalf("врезка без ru: %v", blocks)
	}
}

func TestJournaledConversationDecoratesQuotes(t *testing.T) {
	s := newTestServer(t, nil, nil)
	writeJournal(t, s.Paths.History, convID,
		chat.Record{Role: "user", Text: "вопрос"},
		chat.Record{Role: "assistant", Text: "Начало.\n" +
			">> cite: Nicomachean Ethics II.1\n>> en: habit\n>> ru: привычка\n" +
			">> cite: On Youth and Old Age, On Life and Death, On Breathing 23\n>> en: breath\n>> ru: дыхание\n" +
			">> cite: Lost Treatise 9\n>> en: gone\n>> ru: нет\n" +
			">> вывод: итог\n",
			Quotes: []chat.Quote{
				{Cite: "Nicomachean Ethics II.1", En: "habit", Ru: "привычка", Verified: true, Chapter: ethics.Path},
				{Cite: "On Youth and Old Age, On Life and Death, On Breathing 23", En: "breath", Ru: "дыхание"},
				{Cite: "Lost Treatise 9", En: "gone", Ru: "нет"},
			}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/conversations/"+convID, "")
	var conv struct {
		Items []struct {
			Type   string           `json:"type"`
			Blocks []map[string]any `json:"blocks"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conv); err != nil || len(conv.Items) != 2 {
		t.Fatalf("беседа %s, %v", rec.Body.String(), err)
	}
	quotes := quotesOnly(conv.Items[1].Blocks)
	if len(quotes) != 3 {
		t.Fatalf("врезок %d: %v", len(quotes), conv.Items[1].Blocks)
	}
	if quotes[0]["cite_ru"] != "Никомахова этика II.1" || quotes[0]["work_ru"] != "Никомахова этика" ||
		quotes[0]["cite"] != "Nicomachean Ethics II.1" || quotes[0]["verified"] != true || quotes[0]["chapter"] != ethics.Path {
		t.Errorf("врезка из журнала: %v", quotes[0])
	}
	if quotes[1]["cite_ru"] != "О юности и старости, о жизни и смерти и о дыхании 23" ||
		quotes[1]["work_ru"] != "О юности и старости, о жизни и смерти и о дыхании" {
		t.Errorf("составное название: %v", quotes[1])
	}
	if _, has := quotes[2]["cite_ru"]; has {
		t.Errorf("неизвестная работа: ключ cite_ru есть: %v", quotes[2])
	}
	if _, has := quotes[2]["work_ru"]; has {
		t.Errorf("неизвестная работа: ключ work_ru есть: %v", quotes[2])
	}
	for _, b := range conv.Items[1].Blocks {
		if b["kind"] != "quote" {
			if _, has := b["cite_ru"]; has {
				t.Errorf("у блока %v есть cite_ru", b["kind"])
			}
		}
	}
}

func TestLibraryWorksCarryRussianTitle(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "nicomachean-ethics/ii/01.md", "Nicomachean Ethics", "nicomachean-ethics", "ii", 1, "genuine")
	chapterFile(t, s.Root, "unlisted-work/01.md", "An Unlisted Work", "unlisted-work", "", 1, "genuine")
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/works", "")
	var works []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &works); err != nil || len(works) != 2 {
		t.Fatalf("оглавление %s, %v", rec.Body.String(), err)
	}
	byTitle := map[string]map[string]any{}
	for _, w := range works {
		byTitle[w["title"].(string)] = w
	}
	known, unknown := byTitle["Nicomachean Ethics"], byTitle["An Unlisted Work"]
	if known == nil || unknown == nil {
		t.Fatalf("работы: %v", works)
	}
	if known["title_ru"] != "Никомахова этика" || known["title"] != "Nicomachean Ethics" || known["slug"] != "nicomachean-ethics" {
		t.Errorf("известная работа: %v", known)
	}
	// Работы нет в таблице: английское название остаётся, русского ключа нет, ошибки нет.
	if _, has := unknown["title_ru"]; has {
		t.Errorf("работа вне таблицы: ключ title_ru есть: %v", unknown)
	}
	if unknown["title"] != "An Unlisted Work" {
		t.Errorf("английское название потеряно: %v", unknown)
	}
}

func TestLibraryChapterCarriesRussianWork(t *testing.T) {
	s := newTestServer(t, nil, nil)
	chapterFile(t, s.Root, "nicomachean-ethics/ii/01.md", "Nicomachean Ethics", "nicomachean-ethics", "ii", 1, "genuine")
	chapterFile(t, s.Root, "unlisted-work/01.md", "An Unlisted Work", "unlisted-work", "", 1, "genuine")
	get := func(path string) map[string]any {
		t.Helper()
		rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/chapter?path="+path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: код %d, %s", path, rec.Code, rec.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	known := get("nicomachean-ethics/ii/01.md")
	if known["work_ru"] != "Никомахова этика" || known["work"] != "Nicomachean Ethics" || known["cite"] != "Nicomachean Ethics II.1" {
		t.Errorf("глава известной работы: %v", known)
	}
	unknown := get("unlisted-work/01.md")
	if _, has := unknown["work_ru"]; has {
		t.Errorf("глава работы вне таблицы: ключ work_ru есть: %v", unknown)
	}
	if unknown["work"] != "An Unlisted Work" {
		t.Errorf("английское название потеряно: %v", unknown)
	}
}

func TestLibrarySearchHitsCarryRussianWork(t *testing.T) {
	stranger := corpus.Chapter{Work: "An Unlisted Work", WorkSlug: "unlisted-work", Book: "", Number: 1,
		Group: "ethics", Authenticity: "genuine", Source: "mit", Cite: "An Unlisted Work 1",
		Text: "Words.", Path: "unlisted-work/01.md"}
	s := newTestServer(t, nil, &recFinder{hits: []corpus.Hit{{Chapter: ethics}, {Chapter: stranger}}})
	rec := do(t, s.Handler(), http.MethodGet, "/api/corpus/search?q=habit", "")
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 2 {
		t.Fatalf("%s, %v", rec.Body.String(), err)
	}
	if got[0]["work_ru"] != "Никомахова этика" || got[0]["work"] != "Nicomachean Ethics" || got[0]["cite"] != "Nicomachean Ethics II.1" {
		t.Errorf("попадание известной работы: %v", got[0])
	}
	if _, has := got[1]["work_ru"]; has {
		t.Errorf("попадание работы вне таблицы: ключ work_ru есть: %v", got[1])
	}
	if got[1]["work"] != "An Unlisted Work" || got[1]["cite"] != "An Unlisted Work 1" {
		t.Errorf("английские поля потеряны: %v", got[1])
	}
}

// render.Block — контракт задачи 5: русское название на его проводную форму не
// влияет. Набор тегов сверяется целиком, чтобы добавленное поле было заметно.
func TestRenderBlockWireFormIsUnchanged(t *testing.T) {
	raw, err := json.Marshal(render.Block{Kind: render.BlockQuote, Text: "t", Cite: "Nicomachean Ethics II.1",
		En: "e", Ru: "р", Verified: true, Chapter: "x/01.md"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cite_ru", "work_ru"} {
		if _, has := got[k]; has {
			t.Errorf("в render.Block появился ключ %s: %s", k, raw)
		}
	}
	var tags []string
	typ := reflect.TypeOf(render.Block{})
	for i := 0; i < typ.NumField(); i++ {
		tags = append(tags, typ.Field(i).Tag.Get("json"))
	}
	want := "kind text,omitempty cite,omitempty en,omitempty ru,omitempty verified chapter,omitempty lang,omitempty"
	if strings.Join(tags, " ") != want {
		t.Errorf("теги render.Block: %q, ожидалось %q", strings.Join(tags, " "), want)
	}
}
