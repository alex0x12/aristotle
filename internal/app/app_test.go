package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
	"github.com/alex0x12/aristotle/internal/keys"
	"github.com/alex0x12/aristotle/internal/provider"
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
// текстом (тот же оборванный ход) не должна попадать в историю как
// сообщение без содержимого — она пропускается, как и битая роль.
func TestResumeMessagesSkipsEmptyAssistantText(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "что такое счастье"},
		{Role: "assistant", Text: ""},
		{Role: "user", Text: "повтори"},
		{Role: "assistant", Text: "деятельность души"},
	}
	got := ResumeMessages(records)
	if len(got) != 3 {
		t.Fatalf("ResumeMessages = %+v, ожидалось 3 сообщения (пустой ответ пропущен)", got)
	}
	if got[0].Text != "что такое счастье" || got[1].Text != "повтори" || got[2].Text != "деятельность души" {
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

// TestSwitchProfileKnownChangesProvider закрывает дефект №3: переключение на
// известный профиль обязано создать нового провайдера.
func TestSwitchProfileKnownChangesProvider(t *testing.T) {
	os.Setenv("ARISTOTLE_TEST_DEEPSEEK_KEY", "тестовый-ключ")
	defer os.Unsetenv("ARISTOTLE_TEST_DEEPSEEK_KEY")

	cfg := config.Builtin()
	p := cfg.Profiles["deepseek"]
	p.KeyEnv = "ARISTOTLE_TEST_DEEPSEEK_KEY"
	cfg.Profiles["deepseek"] = p

	profile, prov, err := SwitchProfile(cfg, keys.Store{Path: filepath.Join(t.TempDir(), "keys.json")}, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "deepseek" {
		t.Errorf("Name = %q", profile.Name)
	}
	if prov == nil {
		t.Error("провайдер не создан")
	}
}

// TestSwitchProfileUnknownReturnsError закрывает вторую половину дефекта №3:
// неизвестный профиль обязан вернуть ошибку, не запаниковав и не изменив
// ничего у вызывающей стороны (она просто не подставит нулевые значения).
func TestSwitchProfileUnknownReturnsError(t *testing.T) {
	cfg := config.Builtin()
	profile, prov, err := SwitchProfile(cfg, keys.Store{Path: filepath.Join(t.TempDir(), "keys.json")}, "нет-такого-профиля")
	if err == nil {
		t.Fatal("ожидалась ошибка для неизвестного профиля")
	}
	if prov != nil {
		t.Error("провайдер не должен быть создан при ошибке")
	}
	if profile.Name != "" {
		t.Error("профиль не должен быть заполнен при ошибке")
	}
}

// TestSwitchProfileMissingKeyReturnsError — известный профиль, но без ключа
// в окружении: тоже ошибка, а не паника, и тоже не должен ничего менять.
func TestSwitchProfileMissingKeyReturnsError(t *testing.T) {
	cfg := config.Builtin()
	p := cfg.Profiles["claude"]
	p.KeyEnv = "ARISTOTLE_TEST_MISSING_KEY_FOR_SWITCH"
	cfg.Profiles["claude"] = p
	os.Unsetenv(p.KeyEnv)

	_, prov, err := SwitchProfile(cfg, keys.Store{Path: filepath.Join(t.TempDir(), "keys.json")}, "claude")
	if err == nil {
		t.Fatal("ожидалась ошибка при отсутствии ключа")
	}
	if prov != nil {
		t.Error("провайдер не должен быть создан при отсутствии ключа")
	}
}

func TestProfileNamesSortedAndComplete(t *testing.T) {
	cfg := config.Builtin()
	names := ProfileNames(cfg)
	if len(names) != len(cfg.Profiles) {
		t.Fatalf("ProfileNames вернул %d имён, ожидалось %d", len(names), len(cfg.Profiles))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("список не отсортирован: %v", names)
		}
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

func TestResolveTakesKeyFromFile(t *testing.T) {
	store := keys.Store{Path: filepath.Join(t.TempDir(), "keys.json")}
	if err := store.Set("claude", "sk-ant-из-файла"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Builtin()
	p := cfg.Profiles["claude"]
	p.KeyEnv = "ARISTOTLE_TEST_RESOLVE_KEY"
	cfg.Profiles["claude"] = p
	t.Setenv(p.KeyEnv, "")

	got, err := Resolve(cfg, store, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if k, _ := got.APIKey(); k != "sk-ant-из-файла" || KeySource(got) != "file" {
		t.Fatalf("ключ %q, источник %q", k, KeySource(got))
	}
	t.Setenv(p.KeyEnv, "sk-из-окружения")
	if KeySource(got) != "env" {
		t.Fatalf("источник %q", KeySource(got))
	}
}
