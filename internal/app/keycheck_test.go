package app

import (
	"context"
	"io"
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

// roundTripFunc — транспорт-подделка: проверка ключа Anthropic идёт на
// фиксированный адрес, и в тестах его подменяет транспорт клиента, а не сеть.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(r *http.Request, code int, header http.Header) *http.Response {
	return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: r}
}

// Anthropic-провайдер адрес из профиля не читает (запросы идут на
// api.anthropic.com), поэтому и проверка ключа идёт туда же, что бы ни стояло в
// base_url: правленный вручную адрес невидим в интерфейсе, и ключ не должен
// уйти по нему.
func TestCheckKeyAnthropicUsesFixedOrigin(t *testing.T) {
	var trapHit atomic.Bool
	trap := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { trapHit.Store(true) }))
	defer trap.Close()
	for _, base := range []string{trap.URL, trap.URL + "/", ""} {
		var got []*http.Request
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			got = append(got, r)
			if r.Header.Get("x-api-key") != "sk-ant" || r.Header.Get("anthropic-version") == "" {
				return reply(r, http.StatusUnauthorized, nil), nil
			}
			return reply(r, http.StatusOK, nil), nil
		})}
		p := config.Profile{Name: "c", Kind: config.KindAnthropic, BaseURL: base, Key: "sk-ant"}
		if code, err := CheckKey(context.Background(), p, client); err != nil || code != 200 {
			t.Fatalf("base_url %q: код %d, %v", base, code, err)
		}
		if len(got) != 1 || got[0].URL.String() != "https://api.anthropic.com/v1/models" {
			t.Fatalf("base_url %q: запросы %v", base, got)
		}
	}
	if trapHit.Load() {
		t.Fatal("запрос ушёл на base_url профиля Anthropic")
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
	p := config.Profile{Name: "x", Kind: config.KindOpenAI, BaseURL: srv.URL, Key: "секрет-для-перенаправления"}
	code, err := CheckKey(context.Background(), p, srv.Client())
	if err != nil || code != http.StatusFound {
		t.Fatalf("openai: код %d, %v", code, err)
	}
	if leaked.Load() != nil {
		t.Fatal("openai: ключ ушёл по перенаправлению")
	}

	// Anthropic: адрес фиксирован, перенаправление отдаёт сам «провайдер».
	var requests []string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		return reply(r, http.StatusFound, http.Header{"Location": {other.URL + "/v1/models"}}), nil
	})}
	p = config.Profile{Name: "c", Kind: config.KindAnthropic, Key: "секрет-для-перенаправления"}
	code, err = CheckKey(context.Background(), p, client)
	if err != nil || code != http.StatusFound || len(requests) != 1 {
		t.Fatalf("anthropic: код %d, %v, запросы %v", code, err, requests)
	}
	if leaked.Load() != nil {
		t.Fatal("anthropic: ключ ушёл по перенаправлению")
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
