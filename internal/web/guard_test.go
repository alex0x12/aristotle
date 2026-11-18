package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// Коды, которыми отвечает весь Handler. guardPassed — ответ mux на запрос по
// несуществующему пути: guard запрос пропустил, а маршрута для него нет. Путь
// несуществующий нарочно: ответ не зависит ни от одного обработчика, и таблицы
// не ломаются, когда маршруты API меняются.
const (
	guardOK          = http.StatusOK
	guardPassed      = http.StatusNotFound
	guardForbidden   = http.StatusForbidden
	guardMisdirected = http.StatusMisdirectedRequest
)

// guardRow — один запрос к серверу целиком и код, который должен вернуться.
// Пустые origin и fetchSite — заголовок не ставится вовсе; host ставится
// всегда как есть (пустой — пустой Host).
type guardRow struct {
	name      string
	method    string
	path      string
	host      string
	origin    string
	fetchSite string
	want      int
}

// guardEnv — тестовый сервер и его адреса. Порт берётся у сервера, а не из
// константы: свои адреса строятся от него, как и у самого guard.
type guardEnv struct {
	h    http.Handler
	port string
}

func newGuardEnv(t *testing.T) guardEnv {
	t.Helper()
	s := newTestServer(t, nil, nil)
	return guardEnv{h: s.Handler(), port: strconv.Itoa(s.Port)}
}

// at — имя хоста с портом сервера: at("localhost") = "localhost:8337".
func (e guardEnv) at(host string) string { return host + ":" + e.port }

// otherPort — порт, который сервер не слушает.
func (e guardEnv) otherPort() string {
	p, _ := strconv.Atoi(e.port)
	return strconv.Itoa(p + 1)
}

// run гоняет строки через весь Handler: guard нельзя проверять отдельно от
// маршрутов, потому что его главное свойство — что он стоит перед ними всеми.
// В сообщениях только метод, путь, заголовки и коды: тела ответов не печатаются.
func (e guardEnv) run(t *testing.T, rows []guardRow) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			req := httptest.NewRequest(r.method, r.path, nil)
			req.Host = r.host
			if r.origin != "" {
				req.Header.Set("Origin", r.origin)
			}
			if r.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", r.fetchSite)
			}
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			if rec.Code != r.want {
				t.Errorf("%s %s (Host %q, Origin %q, Sec-Fetch-Site %q): код %d, ожидался %d",
					r.method, r.path, r.host, r.origin, r.fetchSite, rec.Code, r.want)
			}
		})
	}
}

// Origin: что считается «своей страницей» для запроса, меняющего состояние.
func TestGuardOrigin(t *testing.T) {
	e := newGuardEnv(t)
	self := e.at("127.0.0.1")
	e.run(t, []guardRow{
		// Свои три происхождения принимаются.
		{"127.0.0.1", "POST", "/api/нет", self, "http://" + e.at("127.0.0.1"), "", guardPassed},
		{"localhost", "POST", "/api/нет", self, "http://" + e.at("localhost"), "", guardPassed},
		{"[::1]", "POST", "/api/нет", self, "http://" + e.at("[::1]"), "", guardPassed},

		// Чужие и поддельные Origin.
		{"null", "POST", "/api/нет", self, "null", "", guardForbidden},
		{"чужой хост", "POST", "/api/нет", self, "http://evil.example", "", guardForbidden},
		{"суффикс после порта", "POST", "/api/нет", self, "http://" + self + ".evil.com", "", guardForbidden},
		{"userinfo", "POST", "/api/нет", self, "http://" + e.at("localhost") + "@evil.com", "", guardForbidden},
		{"другой порт", "POST", "/api/нет", self, "http://127.0.0.1:" + e.otherPort(), "", guardForbidden},
		{"без порта", "POST", "/api/нет", self, "http://127.0.0.1", "", guardForbidden},
		{"схема https", "POST", "/api/нет", self, "https://" + self, "", guardForbidden},

		// Sec-Fetch-Site подтверждает происхождение только там, где Origin
		// нет вовсе; присутствующий чужой Origin он не спасает.
		{"чужой Origin и same-origin", "POST", "/api/нет", self, "http://evil.example", "same-origin", guardForbidden},
		{"null и same-origin", "POST", "/api/нет", self, "null", "same-origin", guardForbidden},
		{"суффикс и same-origin", "POST", "/api/нет", self, "http://" + self + ".evil.com", "same-origin", guardForbidden},
		{"без Origin, same-origin", "POST", "/api/нет", self, "", "same-origin", guardPassed},
		{"без Origin, cross-site", "POST", "/api/нет", self, "", "cross-site", guardForbidden},
		{"без Origin, same-site", "POST", "/api/нет", self, "", "same-site", guardForbidden},
		{"без Origin, none", "POST", "/api/нет", self, "", "none", guardForbidden},
		{"без Origin и без Sec-Fetch-Site", "POST", "/api/нет", self, "", "", guardForbidden},
	})
}

// Метод: проверка Origin распространяется на всё, кроме GET и HEAD.
func TestGuardMethods(t *testing.T) {
	e := newGuardEnv(t)
	self := e.at("127.0.0.1")
	var rows []guardRow
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		rows = append(rows,
			guardRow{m + " с чужим Origin", m, "/api/нет", self, "http://evil.example", "", guardForbidden},
			guardRow{m + " без Origin", m, "/api/нет", self, "", "", guardForbidden},
			guardRow{m + " со своим Origin", m, "/api/нет", self, "http://" + self, "", guardPassed},
		)
	}
	// Чтение страницы — безопасный запрос: Origin ему не нужен, чужой не мешает.
	for _, m := range []string{"GET", "HEAD"} {
		rows = append(rows,
			guardRow{m + " с чужим Origin", m, "/", self, "http://evil.example", "", guardOK},
			guardRow{m + " с null", m, "/", self, "null", "", guardOK},
			guardRow{m + " без Origin", m, "/", self, "", "", guardOK},
		)
	}
	e.run(t, rows)
}

// Host: точное совпадение строки с одним из трёх своих адресов на своём порту.
func TestGuardHost(t *testing.T) {
	e := newGuardEnv(t)
	e.run(t, []guardRow{
		{"127.0.0.1", "GET", "/", e.at("127.0.0.1"), "", "", guardOK},
		{"localhost", "GET", "/", e.at("localhost"), "", "", guardOK},
		{"[::1]", "GET", "/", e.at("[::1]"), "", "", guardOK},

		{"127.0.0.1 без порта", "GET", "/", "127.0.0.1", "", "", guardMisdirected},
		{"localhost без порта", "GET", "/", "localhost", "", "", guardMisdirected},
		{"[::1] без порта", "GET", "/", "[::1]", "", "", guardMisdirected},
		{"localhost с точкой", "GET", "/", e.at("localhost."), "", "", guardMisdirected},
		{"127.0.0.1 с точкой", "GET", "/", e.at("127.0.0.1."), "", "", guardMisdirected},
		{"другой порт", "GET", "/", "127.0.0.1:" + e.otherPort(), "", "", guardMisdirected},
		{"localhost, другой порт", "GET", "/", "localhost:" + e.otherPort(), "", "", guardMisdirected},
		{"чужое имя", "GET", "/", e.at("evil.example"), "", "", guardMisdirected},
		{"наш адрес с чужим хвостом", "GET", "/", e.at("127.0.0.1") + ".evil.com", "", "", guardMisdirected},
		{"соседний адрес петли", "GET", "/", e.at("127.0.0.2"), "", "", guardMisdirected},
		{"0.0.0.0", "GET", "/", e.at("0.0.0.0"), "", "", guardMisdirected},
		{"пустой Host", "GET", "/", "", "", "", guardMisdirected},

		// Host проверяется и у запросов, меняющих состояние: страница,
		// переназначившая своё имя на 127.0.0.1, приходит и со своим Host, и со
		// своим Origin.
		{"перепривязка DNS", "POST", "/api/нет", e.at("evil.example"), "http://" + e.at("evil.example"), "same-origin", guardMisdirected},
		{"свой Origin, чужой Host", "POST", "/api/нет", e.at("evil.example"), "http://" + e.at("127.0.0.1"), "", guardMisdirected},
	})
}

// guard стоит перед всем: статикой, несуществующими путями и маршрутами API,
// в том числе теми, которых ещё нет.
func TestGuardWrapsEverything(t *testing.T) {
	e := newGuardEnv(t)
	self, bad := e.at("127.0.0.1"), e.at("evil.example")
	e.run(t, []guardRow{
		// Контроль: статика отдаётся при своём Host, иначе строки ниже ничего
		// не доказывают. /static/ — каталог встроенной статики с index.html.
		{"статика, свой Host", "GET", "/static/", self, "", "", guardOK},
		{"статика, чужой Host", "GET", "/static/", bad, "", "", guardMisdirected},
		{"файл статики, чужой Host", "GET", "/static/index.html", bad, "", "", guardMisdirected},
		{"несуществующая статика, чужой Host", "GET", "/static/нет.js", bad, "", "", guardMisdirected},
		{"404, свой Host", "GET", "/нет-такого", self, "", "", http.StatusNotFound},
		{"404, чужой Host", "GET", "/нет-такого", bad, "", "", guardMisdirected},
		{"API, чужой Host", "GET", "/api/state", bad, "", "", guardMisdirected},
		{"неизвестный API, POST без Origin", "POST", "/api/нет-такого", self, "", "", guardForbidden},
		{"неизвестный API, POST с чужим Origin", "POST", "/api/нет-такого", self, "http://evil.example", "", guardForbidden},
		{"существующий маршрут, POST без Origin", "POST", "/api/conversations", self, "", "", guardForbidden},
		{"существующий маршрут, DELETE с чужим Origin", "DELETE", "/api/conversations/" + convID, self, "http://evil.example", "", guardForbidden},
		{"существующий маршрут, PATCH без Origin", "PATCH", "/api/conversations/" + convID, self, "", "", guardForbidden},
		// Маршрут только для GET, метод другой: отказ даёт guard, а не mux (405).
		{"POST на маршрут только для GET, без Origin", "POST", "/api/state", self, "", "", guardForbidden},
	})
}

// guard берёт порт из Server.Port, а не из константы.
func TestGuardUsesServerPort(t *testing.T) {
	s := newTestServer(t, nil, nil)
	old := strconv.Itoa(s.Port)
	s.Port = 4242
	e := guardEnv{h: s.Handler(), port: "4242"}
	e.run(t, []guardRow{
		{"свой Host на новом порту", "GET", "/", "127.0.0.1:4242", "", "", guardOK},
		{"[::1] на новом порту", "GET", "/", "[::1]:4242", "", "", guardOK},
		{"прежний порт стал чужим", "GET", "/", "127.0.0.1:" + old, "", "", guardMisdirected},
		{"свой Origin на новом порту", "POST", "/api/нет", "127.0.0.1:4242", "http://127.0.0.1:4242", "", guardPassed},
		{"Origin с прежним портом", "POST", "/api/нет", "127.0.0.1:4242", "http://127.0.0.1:" + old, "", guardForbidden},
	})
}
