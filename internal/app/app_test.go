package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/provider"
	"github.com/alex0x12/aristotle/internal/render"
)

// TestResumeMessagesRestoresRoles закрывает дефект №2 итогового ревью:
// --resume менял только путь для дозаписи, но никогда не читал файл назад
// в Session.Messages. Здесь проверяется чистое преобразование записей
// журнала в сообщения провайдера.
func TestResumeMessagesRestoresRoles(t *testing.T) {
	records := []chat.Record{
		{Time: time.Now(), Role: "user", Text: "что такое счастье"},
		{Time: time.Now(), Role: "assistant", Text: "деятельность души"},
		{Time: time.Now(), Role: "user", Text: "и что из этого следует?"},
		{Time: time.Now(), Role: "assistant", Text: "стало быть, счастье — дело"},
	}
	got := ResumeMessages(records)
	if len(got) != 4 {
		t.Fatalf("ResumeMessages вернул %d сообщений, ожидалось 4", len(got))
	}
	want := []struct {
		role provider.Role
		text string
	}{
		{provider.RoleUser, "что такое счастье"},
		{provider.RoleAssistant, "деятельность души"},
		{provider.RoleUser, "и что из этого следует?"},
		{provider.RoleAssistant, "стало быть, счастье — дело"},
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Text != w.text {
			t.Errorf("сообщение %d = %+v, ожидалось {%s %s}", i, got[i], w.role, w.text)
		}
	}
}

// TestResumeMessagesDropsTrailingUnansweredUser закрывает находку итогового
// ревью: история обрывается репликой user после ЛЮБОЙ ошибки провайдера или
// Ctrl-C — главный случай, ради которого вообще возобновляют беседу.
// Восстановленные сообщения не должны заканчиваться ролью user: следующий
// ход дописал бы вторую подряд реплику user, а API такое отвергает.
func TestResumeMessagesDropsTrailingUnansweredUser(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "что такое счастье"},
		{Role: "assistant", Text: "деятельность души"},
		{Role: "user", Text: "оборванный вопрос без ответа"},
	}
	got := ResumeMessages(records)
	if len(got) != 2 {
		t.Fatalf("ResumeMessages вернул %d сообщений, ожидалось 2 (хвост без ответа отброшен): %+v", len(got), got)
	}
	if got[len(got)-1].Role != provider.RoleAssistant {
		t.Fatalf("последнее сообщение должно быть от assistant, получено %+v", got[len(got)-1])
	}
}

// TestResumeMessagesDropsAllLeadingIfNeverAnswered: вся история — один
// незавершённый ход (первый же вопрос оборвался ошибкой). Результат должен
// быть пуст, а не одно висящее сообщение user.
func TestResumeMessagesDropsAllLeadingIfNeverAnswered(t *testing.T) {
	records := []chat.Record{{Role: "user", Text: "единственный вопрос"}}
	got := ResumeMessages(records)
	if len(got) != 0 {
		t.Fatalf("ResumeMessages = %+v, ожидалась пустая история", got)
	}
}

// TestResumeMessagesSkipsEmptyAssistantText: запись ассистента с пустым
// текстом — оборванный ход. Сообщение без содержимого провайдер не примет, а
// оставшийся без ответа вопрос дал бы два user подряд, поэтому ход выпадает
// целиком: вопрос тоже. Так же Turn откатывает ход в живой сессии.
func TestResumeMessagesSkipsEmptyAssistantText(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "что такое счастье"},
		{Role: "assistant", Text: ""},
		{Role: "user", Text: "повтори"},
		{Role: "assistant", Text: "деятельность души"},
	}
	got := ResumeMessages(records)
	if len(got) != 2 {
		t.Fatalf("ResumeMessages = %+v, ожидалось 2 сообщения (оборванный ход выпал целиком)", got)
	}
	if got[0].Text != "повтори" || got[1].Text != "деятельность души" {
		t.Fatalf("ResumeMessages = %+v", got)
	}
}

func TestResumeMessagesSkipsUnknownRoles(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "х"},
		{Role: "", Text: "битая запись"},
		{Role: "system", Text: "тоже не наша роль"},
		{Role: "assistant", Text: "ответ"},
	}
	got := ResumeMessages(records)
	if len(got) != 2 || got[0].Text != "х" || got[1].Text != "ответ" {
		t.Fatalf("ResumeMessages = %+v, ожидались только записи user и assistant", got)
	}
}

func TestResumeMessagesEmptyGivesEmpty(t *testing.T) {
	if got := ResumeMessages(nil); len(got) != 0 {
		t.Fatalf("ResumeMessages(nil) = %+v", got)
	}
}

// TestLastUserMessageFindsMostRecent закрывает дефект №4: /again обязан
// повторить последний вопрос ПОЛЬЗОВАТЕЛЯ, а не последнее сообщение вообще
// (последним в Messages обычно стоит ответ ассистента).
func TestLastUserMessageFindsMostRecent(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Text: "первый вопрос"},
		{Role: provider.RoleAssistant, Text: "первый ответ"},
		{Role: provider.RoleUser, Text: "второй вопрос"},
		{Role: provider.RoleAssistant, Text: "второй ответ"},
	}
	if got := LastUserMessage(msgs); got != "второй вопрос" {
		t.Fatalf("LastUserMessage = %q, ожидалось %q", got, "второй вопрос")
	}
}

func TestLastUserMessageEmptyHistoryGivesEmptyString(t *testing.T) {
	if got := LastUserMessage(nil); got != "" {
		t.Fatalf("LastUserMessage(nil) = %q, ожидалась пустая строка", got)
	}
}

// TestLastUserMessageSkipsToolResultCarriers закрывает живой дефект,
// найденный при ручном прогоне: chat.Session.Turn кладёт результаты
// инструмента в Messages с Role: RoleUser (см. internal/chat/loop.go) —
// это не реплика человека, а служебное сообщение с пустым Text и
// заполненным ToolResults. Если ход задействовал поиск, такое сообщение
// оказывается последним с Role==RoleUser, и наивный поиск "последнего
// user" вместо вопроса находит пустую строку — /again в живом прогоне
// после хода с поиском отвечал "истории нет, нечего повторять" вместо
// повтора настоящего вопроса.
func TestLastUserMessageSkipsToolResultCarriers(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Text: "что такое эвдемония"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}},
		{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{ID: "c1", Content: "..."}}},
		{Role: provider.RoleAssistant, Text: "эвдемония — деятельность души"},
	}
	if got := LastUserMessage(msgs); got != "что такое эвдемония" {
		t.Fatalf("LastUserMessage = %q, ожидалось %q (результат инструмента не должен считаться вопросом)",
			got, "что такое эвдемония")
	}
}

func TestDefaultPathsUnderHome(t *testing.T) {
	t.Setenv("HOME", "/home/тест")
	p, err := DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.Config != "/home/тест/.config/aristotle/config.json" ||
		p.Keys != "/home/тест/.config/aristotle/keys.json" ||
		p.History != "/home/тест/.local/share/aristotle" {
		t.Fatalf("пути = %+v", p)
	}
}

func TestNewHistoryPathIsUTCStamp(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	got := NewHistoryPath("/жур", time.Date(2026, 11, 18, 13, 4, 5, 0, msk))
	if got != "/жур/2026-11-18T10-04-05.jsonl" {
		t.Fatalf("NewHistoryPath = %q", got)
	}
}

func TestDropLastTurnRemovesFromLastQuestion(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Text: "первый"},
		{Role: provider.RoleAssistant, Text: "ответ"},
		{Role: provider.RoleUser, Text: "второй"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "t"}}},
		{Role: provider.RoleUser, ToolResults: []provider.ToolResult{{ID: "t"}}},
		{Role: provider.RoleAssistant, Text: "второй ответ"},
	}
	got := DropLastTurn(msgs)
	if len(got) != 2 || got[1].Text != "ответ" {
		t.Fatalf("DropLastTurn = %+v", got)
	}
}

func TestResumeMessagesHonoursRetract(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "вопрос"},
		{Role: "assistant", Text: "первый ответ"},
		{Role: chat.RoleRetract},
		{Role: "user", Text: "вопрос"},
		{Role: "assistant", Text: "второй ответ"},
		{Role: chat.RoleMeta, Title: "заголовок"},
		{Role: chat.RoleTrim, Excerpts: 1},
	}
	got := ResumeMessages(records)
	if len(got) != 2 || got[1].Text != "второй ответ" {
		t.Fatalf("ResumeMessages = %+v", got)
	}
}

// В журнал идут только врезки: проза и пометки восстанавливаются из текста
// ответа, а сверка — нет.
func TestQuotesOfKeepsOnlyQuotes(t *testing.T) {
	blocks := []render.Block{
		{Kind: render.BlockProse, Text: "вступление"},
		{Kind: render.BlockQuote, Cite: "Nicomachean Ethics II.1", En: "habit", Ru: "привычка",
			Verified: true, Chapter: "nicomachean-ethics/ii/01.md"},
		{Kind: "вывод", Text: "середина"},
		{Kind: render.BlockQuote, Cite: "Politics I.2", En: "polis"},
	}
	want := []chat.Quote{
		{Cite: "Nicomachean Ethics II.1", En: "habit", Ru: "привычка", Verified: true,
			Chapter: "nicomachean-ethics/ii/01.md"},
		{Cite: "Politics I.2", En: "polis"},
	}
	got := QuotesOf(blocks)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("QuotesOf = %+v, ожидалось %+v", got, want)
	}
	if QuotesOf(nil) != nil {
		t.Fatal("без врезок результат пуст")
	}
}

// flat — сообщения одной строкой «роль:текст», чтобы сравнивать историю целиком.
func flat(msgs []provider.Message) string {
	out := ""
	for i, m := range msgs {
		if i > 0 {
			out += " | "
		}
		out += string(m.Role) + ":" + m.Text
	}
	return out
}

// Живая сессия откатывает ход с ошибкой и ход, остановленный до первого слова
// (chat.Turn), поэтому после перезапуска такой ход не должен возвращаться —
// иначе рядом окажутся два вопроса подряд, а часть провайдеров такое отвергает.
func TestResumeMessagesRollsBackFailedAndEmptyStoppedTurns(t *testing.T) {
	cases := []struct {
		name string
		stop chat.Record // запись ответа на второй вопрос
	}{
		{"остановлен до первого слова", chat.Record{Role: "assistant", StopReason: "cancelled"}},
		{"ошибка без текста", chat.Record{Role: "assistant", StopReason: "error"}},
		{"ошибка с началом ответа", chat.Record{Role: "assistant", Text: "начало ответа", StopReason: "error"}},
	}
	for _, c := range cases {
		records := []chat.Record{
			{Role: "user", Text: "q1"},
			{Role: "assistant", Text: "a1", StopReason: "end"},
			{Role: "user", Text: "q2"},
			c.stop,
			{Role: "user", Text: "q3"},
			{Role: "assistant", Text: "a3", StopReason: "end"},
		}
		got := flat(ResumeMessages(records))
		want := "user:q1 | assistant:a1 | user:q3 | assistant:a3"
		if got != want {
			t.Errorf("%s: %s, ожидалось %s", c.name, got, want)
		}
	}
}

// Остановленный ход с началом ответа живая сессия оставляет: вопрос и
// сказанное, чтобы «продолжай» после Stop имело смысл. Так же его
// восстанавливает журнал.
func TestResumeMessagesKeepsStoppedTurnWithPartialAnswer(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "q1"},
		{Role: "assistant", Text: "a1", StopReason: "end"},
		{Role: "user", Text: "q2"},
		{Role: "assistant", Text: "начало ответа", StopReason: "cancelled"},
		{Role: "user", Text: "продолжай"},
		{Role: "assistant", Text: "a3", StopReason: "end"},
	}
	got := flat(ResumeMessages(records))
	want := "user:q1 | assistant:a1 | user:q2 | assistant:начало ответа | user:продолжай | assistant:a3"
	if got != want {
		t.Fatalf("%s, ожидалось %s", got, want)
	}
}

// retract отзывает последний вопрос журнала. Если этот ход откатан, в истории
// модели его нет, и отзыв не должен снять предыдущий, удавшийся ход; если ход
// остановлен с текстом и в истории стоит, отзыв снимает ровно эту пару.
func TestResumeMessagesRetractAfterRolledBackTurnKeepsEarlierTurn(t *testing.T) {
	cases := []struct {
		name string
		stop chat.Record
	}{
		{"после ошибки", chat.Record{Role: "assistant", StopReason: "error"}},
		{"после ошибки с началом ответа", chat.Record{Role: "assistant", Text: "начало", StopReason: "error"}},
		{"после остановки с текстом", chat.Record{Role: "assistant", Text: "начало", StopReason: "cancelled"}},
		{"после остановки без текста", chat.Record{Role: "assistant", StopReason: "cancelled"}},
	}
	for _, c := range cases {
		base := []chat.Record{
			{Role: "user", Text: "q1"},
			{Role: "assistant", Text: "a1", StopReason: "end"},
			{Role: "user", Text: "q2"},
			c.stop,
			{Role: chat.RoleRetract},
		}
		if got, want := flat(ResumeMessages(base)), "user:q1 | assistant:a1"; got != want {
			t.Errorf("%s, отзыв последним: %s, ожидалось %s", c.name, got, want)
		}
		again := append(append([]chat.Record{}, base...),
			chat.Record{Role: "user", Text: "q2"},
			chat.Record{Role: "assistant", Text: "a2", StopReason: "end"})
		if got, want := flat(ResumeMessages(again)), "user:q1 | assistant:a1 | user:q2 | assistant:a2"; got != want {
			t.Errorf("%s, потом ответ: %s, ожидалось %s", c.name, got, want)
		}
	}
}

func TestResumeMessagesRetractAfterKeptTurnDropsIt(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "q1"},
		{Role: "assistant", Text: "a1", StopReason: "end"},
		{Role: "user", Text: "q2"},
		{Role: "assistant", Text: "a2", StopReason: "end"},
		{Role: chat.RoleRetract},
		{Role: "user", Text: "q2"},
		{Role: "assistant", Text: "a2 снова", StopReason: "end"},
	}
	if got, want := flat(ResumeMessages(records)), "user:q1 | assistant:a1 | user:q2 | assistant:a2 снова"; got != want {
		t.Fatalf("%s, ожидалось %s", got, want)
	}
	// Два отзыва подряд (между ними вопрос не записан): снимаются два хода.
	records = append(records[:4:4], chat.Record{Role: chat.RoleRetract}, chat.Record{Role: chat.RoleRetract})
	if got := flat(ResumeMessages(records)); got != "" {
		t.Fatalf("%q, ожидалась пустая история", got)
	}
}

// Журналы командной строки не знают про stop_reason: ответ с текстом
// остаётся, служебные записи ничего не меняют.
func TestResumeMessagesOldStyleJournalUnchanged(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "q1"},
		{Role: "assistant", Text: "a1"},
		{Role: chat.RoleMeta, Title: "заголовок"},
		{Role: "user", Text: "q2"},
		{Role: "assistant", Text: "a2", StopReason: "end"},
		{Role: chat.RoleTrim, Excerpts: 1},
	}
	if got, want := flat(ResumeMessages(records)), "user:q1 | assistant:a1 | user:q2 | assistant:a2"; got != want {
		t.Fatalf("%s, ожидалось %s", got, want)
	}
}

// Вопрос без ответа посреди журнала (процесс убит на ходу) не должен давать
// два user подряд; ответ без вопроса (потерянная битая строка) пропускается.
func TestResumeMessagesKeepsAlternationOnDamagedJournal(t *testing.T) {
	records := []chat.Record{
		{Role: "assistant", Text: "осиротевший ответ"},
		{Role: "user", Text: "q1"},
		{Role: "user", Text: "q2"},
		{Role: "assistant", Text: "a2", StopReason: "end"},
	}
	if got, want := flat(ResumeMessages(records)), "user:q2 | assistant:a2"; got != want {
		t.Fatalf("%s, ожидалось %s", got, want)
	}
}

func TestLastTurnKept(t *testing.T) {
	u := chat.Record{Role: "user", Text: "q"}
	cases := []struct {
		name string
		recs []chat.Record
		want bool
	}{
		{"нет записей", nil, false},
		{"ход закончился", []chat.Record{u, {Role: "assistant", Text: "а", StopReason: "end"}}, true},
		{"ошибка", []chat.Record{u, {Role: "assistant", StopReason: "error"}}, false},
		{"ошибка с началом ответа", []chat.Record{u, {Role: "assistant", Text: "начало", StopReason: "error"}}, false},
		{"остановка с началом ответа", []chat.Record{u, {Role: "assistant", Text: "начало", StopReason: "cancelled"}}, true},
		{"остановка до первого слова", []chat.Record{u, {Role: "assistant", StopReason: "cancelled"}}, false},
		{"пустой ответ", []chat.Record{u, {Role: "assistant", StopReason: "end"}}, false},
		{"запись командной строки", []chat.Record{u, {Role: "assistant", Text: "а"}}, true},
		{"ответа нет", []chat.Record{u, {Role: "assistant", Text: "а", StopReason: "end"}, u}, false},
		{"служебные записи в хвосте", []chat.Record{u, {Role: "assistant", Text: "а", StopReason: "end"}, {Role: chat.RoleMeta, Title: "т"}}, true},
		{"ход после сбоя", []chat.Record{u, {Role: "assistant", StopReason: "error"}, {Role: chat.RoleRetract}, u, {Role: "assistant", Text: "а", StopReason: "end"}}, true},
		{"отзыв остановленного с текстом: последним стоит предыдущий", []chat.Record{
			u, {Role: "assistant", Text: "а", StopReason: "end"}, u, {Role: "assistant", Text: "начало", StopReason: "cancelled"}, {Role: chat.RoleRetract}}, true},
		{"отзыв последнего хода журнала", []chat.Record{
			u, {Role: "assistant", Text: "а", StopReason: "end"}, u, {Role: "assistant", StopReason: "error"}, {Role: chat.RoleRetract}}, true},
	}
	for _, c := range cases {
		if got := LastTurnKept(c.recs); got != c.want {
			t.Errorf("%s: %v, ожидалось %v", c.name, got, c.want)
		}
	}
}

// Ответ из одних пробельных символов — тот же пустой ответ: Anthropic отвергает
// текстовый блок без видимого текста, а Stop сразу после одной дельты «\n»
// даёт именно такой. Правило одно на живую сессию и на восстановление.
func TestAnswerKeptTreatsWhitespaceAsEmpty(t *testing.T) {
	cases := []struct {
		name string
		rec  chat.Record
		want bool
	}{
		{"перевод строки, остановка", chat.Record{Text: "\n", StopReason: "cancelled"}, false},
		{"перевод строки, конец", chat.Record{Text: "\n", StopReason: "end"}, false},
		{"пробелы и табуляция, остановка", chat.Record{Text: "  \n\t", StopReason: "cancelled"}, false},
		{"пробелы и табуляция, конец", chat.Record{Text: "  \n\t", StopReason: "end"}, false},
		{"пробелы, запись командной строки", chat.Record{Text: " \r\n"}, false},
		{"пусто, конец", chat.Record{StopReason: "end"}, false},
		{"текст, остановка", chat.Record{Text: "начало", StopReason: "cancelled"}, true},
		{"текст, конец", chat.Record{Text: "ответ", StopReason: "end"}, true},
		{"текст в пробелах", chat.Record{Text: "\n ответ \n", StopReason: "end"}, true},
		{"текст, запись командной строки", chat.Record{Text: "ответ"}, true},
		{"текст, ошибка", chat.Record{Text: "начало", StopReason: "error"}, false},
	}
	for _, c := range cases {
		if got := AnswerKept(c.rec); got != c.want {
			t.Errorf("%s: AnswerKept = %v, ожидалось %v", c.name, got, c.want)
		}
	}
}

// Ход с пробельным ответом выпадает при восстановлении целиком, вместе с
// вопросом: роли по-прежнему чередуются, а «ещё раз» после него не снимает
// предыдущий удавшийся ход.
func TestResumeMessagesRollsBackWhitespaceOnlyAnswer(t *testing.T) {
	for _, text := range []string{"\n", "  \n\t"} {
		for _, reason := range []string{"cancelled", "end"} {
			name := "текст " + strconv.Quote(text) + ", " + reason
			blank := chat.Record{Role: "assistant", Text: text, StopReason: reason}
			records := []chat.Record{
				{Role: "user", Text: "q1"},
				{Role: "assistant", Text: "a1", StopReason: "end"},
				{Role: "user", Text: "q2"},
				blank,
				{Role: "user", Text: "q3"},
				{Role: "assistant", Text: "a3", StopReason: "end"},
			}
			msgs := ResumeMessages(records)
			if got, want := flat(msgs), "user:q1 | assistant:a1 | user:q3 | assistant:a3"; got != want {
				t.Errorf("%s: %s, ожидалось %s", name, got, want)
			}
			for i, m := range msgs {
				if wantRole := []provider.Role{provider.RoleUser, provider.RoleAssistant}[i%2]; m.Role != wantRole {
					t.Errorf("%s: сообщение %d с ролью %s, роли должны чередоваться", name, i, m.Role)
				}
			}

			// Хвост журнала: ход с пробельным ответом последний.
			tail := records[:4]
			if got, want := flat(ResumeMessages(tail)), "user:q1 | assistant:a1"; got != want {
				t.Errorf("%s, ход последний: %s, ожидалось %s", name, got, want)
			}
			if LastTurnKept(tail) {
				t.Errorf("%s: LastTurnKept = true для хода, которого в истории нет", name)
			}

			// Отзыв после такого хода не должен снять предыдущий, удавшийся ход:
			// откатанного в истории нет.
			again := append(append([]chat.Record{}, tail...), chat.Record{Role: chat.RoleRetract})
			if got, want := flat(ResumeMessages(again)), "user:q1 | assistant:a1"; got != want {
				t.Errorf("%s, отзыв: %s, ожидалось %s", name, got, want)
			}
			if !LastTurnKept(again) {
				t.Errorf("%s: после отзыва последним стоит удавшийся ход, LastTurnKept = false", name)
			}
		}
	}
}
