package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Проверка ключа ходит только к поддельным провайдерам httptest на loopback:
// настоящих ключей и настоящих адресов в этих тестах нет.

func TestCheckKeySendsOnlyBearerToConfiguredAddress(t *testing.T) {
	var mu sync.Mutex
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r
		mu.Unlock()
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer FAKE-верный" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	// Хвостовой слэш адреса не даёт «//models».
	if code, err := CheckKey(context.Background(), srv.URL+"/v1/", "FAKE-верный", srv.Client()); err != nil || code != 200 {
		t.Fatalf("код %d, %v", code, err)
	}
	mu.Lock()
	req := got
	mu.Unlock()
	if req.Method != http.MethodGet {
		t.Fatalf("метод %s", req.Method)
	}
	for name := range req.Header {
		switch http.CanonicalHeaderKey(name) {
		case "Authorization", "User-Agent", "Accept-Encoding":
		default:
			t.Errorf("лишний заголовок %s", name)
		}
	}
	if code, _ := CheckKey(context.Background(), srv.URL+"/v1", "FAKE-чужой", srv.Client()); code != 401 {
		t.Fatalf("код %d, ожидался 401", code)
	}
}

// Без ключа — ErrNoKey до всякого запроса.
func TestCheckKeyWithoutKeyDoesNotCallProvider(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer srv.Close()
	_, err := CheckKey(context.Background(), srv.URL, "", srv.Client())
	if err != ErrNoKey {
		t.Fatalf("ошибка %v, ожидалась ErrNoKey", err)
	}
	if called.Load() {
		t.Fatal("запрос ушёл без ключа")
	}
}

// Недопустимый адрес отвергается так же, как при сохранении настроек, и ключ
// никуда не уходит.
func TestCheckKeyRejectsBadAddressBeforeAnyRequest(t *testing.T) {
	for _, base := range []string{"", "ftp://x/v1", "file:///etc/passwd", "https://u:p@host/v1", "host/v1", "http://a b/v1"} {
		if _, err := CheckKey(context.Background(), base, "FAKE-KEY", http.DefaultClient); err == nil {
			t.Errorf("адрес %q принят", base)
		}
	}
}

// Перенаправление не идёт: заголовок Authorization мог бы уйти по чужому
// адресу (в пределах того же хоста Go его не срезает). Ответ 3xx — просто
// отказ с кодом.
func TestCheckKeyDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Value // string: сервер пишет из своей горутины
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/models", http.StatusFound)
	}))
	defer srv.Close()
	code, err := CheckKey(context.Background(), srv.URL, "FAKE-секрет-для-перенаправления", srv.Client())
	if err != nil || code != http.StatusFound {
		t.Fatalf("код %d, %v", code, err)
	}
	if leaked.Load() != nil {
		t.Fatal("ключ ушёл по перенаправлению")
	}
}

// Клиент вызывающего не меняется: запрет перенаправлений — на копии.
func TestCheckKeyDoesNotMutateCallersClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	client := &http.Client{}
	if _, err := CheckKey(context.Background(), srv.URL, "FAKE-KEY", client); err != nil {
		t.Fatal(err)
	}
	if client.CheckRedirect != nil {
		t.Fatal("CheckKey изменил клиент вызывающего")
	}
}

// Недоступный адрес — ошибка, и в тексте ошибки ключа нет.
func TestCheckKeyErrorHasNoKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // порт закрыт
	_, err := CheckKey(context.Background(), url, "очень-секретный-ключ", http.DefaultClient)
	if err == nil {
		t.Fatal("ожидалась ошибка соединения")
	}
	if strings.Contains(err.Error(), "очень-секретный-ключ") {
		t.Fatal("ключ в тексте ошибки")
	}
}

// Тело ответа не доходит до вызывающего и не читается без предела.
func TestCheckKeyReturnsStatusOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"bad key ` + r.Header.Get("Authorization") + `"}`))
	}))
	defer srv.Close()
	code, err := CheckKey(context.Background(), srv.URL, "FAKE-KEY", srv.Client())
	if code != http.StatusForbidden || err != nil {
		t.Fatalf("код %d, %v", code, err)
	}
}
