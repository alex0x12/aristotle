package main

import (
	"os"
	"testing"
	"time"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/config"
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
	}
	got := resumeMessages(records)
	if len(got) != 3 {
		t.Fatalf("resumeMessages вернул %d сообщений, ожидалось 3", len(got))
	}
	want := []struct {
		role provider.Role
		text string
	}{
		{provider.RoleUser, "что такое счастье"},
		{provider.RoleAssistant, "деятельность души"},
		{provider.RoleUser, "и что из этого следует?"},
	}
	for i, w := range want {
		if got[i].Role != w.role || got[i].Text != w.text {
			t.Errorf("сообщение %d = %+v, ожидалось {%s %s}", i, got[i], w.role, w.text)
		}
	}
}

func TestResumeMessagesSkipsUnknownRoles(t *testing.T) {
	records := []chat.Record{
		{Role: "user", Text: "х"},
		{Role: "", Text: "битая запись"},
		{Role: "system", Text: "тоже не наша роль"},
	}
	got := resumeMessages(records)
	if len(got) != 1 || got[0].Text != "х" {
		t.Fatalf("resumeMessages = %+v, ожидалась только одна запись user", got)
	}
}

func TestResumeMessagesEmptyGivesEmpty(t *testing.T) {
	if got := resumeMessages(nil); len(got) != 0 {
		t.Fatalf("resumeMessages(nil) = %+v", got)
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
	if got := lastUserMessage(msgs); got != "второй вопрос" {
		t.Fatalf("lastUserMessage = %q, ожидалось %q", got, "второй вопрос")
	}
}

func TestLastUserMessageEmptyHistoryGivesEmptyString(t *testing.T) {
	if got := lastUserMessage(nil); got != "" {
		t.Fatalf("lastUserMessage(nil) = %q, ожидалась пустая строка", got)
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
	if got := lastUserMessage(msgs); got != "что такое эвдемония" {
		t.Fatalf("lastUserMessage = %q, ожидалось %q (результат инструмента не должен считаться вопросом)",
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

	profile, prov, err := switchProfile(cfg, "deepseek")
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
	profile, prov, err := switchProfile(cfg, "нет-такого-профиля")
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

	_, prov, err := switchProfile(cfg, "claude")
	if err == nil {
		t.Fatal("ожидалась ошибка при отсутствии ключа")
	}
	if prov != nil {
		t.Error("провайдер не должен быть создан при отсутствии ключа")
	}
}

func TestProfileNamesSortedAndComplete(t *testing.T) {
	cfg := config.Builtin()
	names := profileNames(cfg)
	if len(names) != len(cfg.Profiles) {
		t.Fatalf("profileNames вернул %d имён, ожидалось %d", len(names), len(cfg.Profiles))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("список не отсортирован: %v", names)
		}
	}
}
