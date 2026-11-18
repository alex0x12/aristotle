package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alex0x12/aristotle/internal/config"
)

// Проверка ключа ходит только к поддельным провайдерам httptest: настоящих
// ключей и настоящих адресов в этих тестах нет.

func TestCheckKeyOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer верный" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	p := config.Profile{Name: "x", Kind: config.KindOpenAI, BaseURL: srv.URL + "/v1", Key: "верный"}
	if code, err := CheckKey(context.Background(), p, srv.Client()); err != nil || code != 200 {
		t.Fatalf("код %d, %v", code, err)
	}
	p.Key = "чужой"
	if code, _ := CheckKey(context.Background(), p, srv.Client()); code != 401 {
		t.Fatalf("код %d, ожидался 401", code)
	}
}

func TestCheckKeyAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "sk-ant" || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	p := config.Profile{Name: "c", Kind: config.KindAnthropic, BaseURL: srv.URL, Key: "sk-ant"}
	if code, err := CheckKey(context.Background(), p, srv.Client()); err != nil || code != 200 {
		t.Fatalf("код %d, %v", code, err)
	}
}

// Ключ приходит из окружения профиля так же, как и при настоящем запросе.
func TestCheckKeyUsesEnvKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer из-окружения" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	t.Setenv("ARISTOTLE_TEST_CHECK_KEY", "из-окружения")
	p := config.Profile{Name: "x", Kind: config.KindOpenAI, BaseURL: srv.URL, KeyEnv: "ARISTOTLE_TEST_CHECK_KEY"}
	if code, err := CheckKey(context.Background(), p, srv.Client()); err != nil || code != 200 {
		t.Fatalf("код %d, %v", code, err)
	}
}

// Профиль без ключа — ошибка до всякого запроса.
func TestCheckKeyWithoutKeyDoesNotCallProvider(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer srv.Close()
	t.Setenv("ARISTOTLE_TEST_CHECK_NOKEY", "")
	p := config.Profile{Name: "x", Kind: config.KindOpenAI, BaseURL: srv.URL, KeyEnv: "ARISTOTLE_TEST_CHECK_NOKEY"}
	if _, err := CheckKey(context.Background(), p, srv.Client()); err == nil {
		t.Fatal("ожидалась ошибка: ключа нет")
	}
	if called.Load() {
		t.Fatal("запрос ушёл без ключа")
	}
}

// Перенаправление не идёт: заголовок x-api-key Go при переходе на другой
// адрес не срезает, и ключ ушёл бы туда, куда его не посылали. Ответ 3xx —
// это просто отказ с кодом.
func TestCheckKeyDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Value // string: сервер пишет из своей горутины
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("x-api-key") + r.Header.Get("Authorization"))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	defer srv.Close()
	for _, kind := range []string{config.KindAnthropic, config.KindOpenAI} {
		p := config.Profile{Name: "x", Kind: kind, BaseURL: srv.URL, Key: "секрет-для-перенаправления"}
		code, err := CheckKey(context.Background(), p, srv.Client())
		if err != nil || code != http.StatusFound {
			t.Fatalf("%s: код %d, %v", kind, code, err)
		}
		if leaked.Load() != nil {
			t.Fatalf("%s: ключ ушёл по перенаправлению", kind)
		}
	}
}

// Недоступный адрес — ошибка, и в тексте ошибки ключа нет.
func TestCheckKeyErrorHasNoKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // порт закрыт
	p := config.Profile{Name: "x", Kind: config.KindOpenAI, BaseURL: url, Key: "очень-секретный-ключ"}
	_, err := CheckKey(context.Background(), p, http.DefaultClient)
	if err == nil {
		t.Fatal("ожидалась ошибка соединения")
	}
	if strings.Contains(err.Error(), "очень-секретный-ключ") {
		t.Fatal("ключ в тексте ошибки")
	}
}
