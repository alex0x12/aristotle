package web

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/alex0x12/aristotle/internal/corpus"
)

type chapterRef struct {
	Number int    `json:"number"`
	Cite   string `json:"cite"`
	Path   string `json:"path"`
}

type bookRef struct {
	Book     string       `json:"book"`
	Chapters []chapterRef `json:"chapters"`
}

type workRef struct {
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	Group        string    `json:"group"`
	Authenticity string    `json:"authenticity"`
	Translator   string    `json:"translator,omitempty"`
	Books        []bookRef `json:"books"`
}

// roman — значение римской цифры книги (i, ii, … xiv); 0 — не римская.
func roman(s string) int {
	vals := map[rune]int{'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100}
	r := []rune(strings.ToLower(s))
	total, prev := 0, 0
	for i := len(r) - 1; i >= 0; i-- {
		v, ok := vals[r[i]]
		if !ok {
			return 0
		}
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return total
}

func bookLess(a, b string) bool {
	ra, rb := roman(a), roman(b)
	if ra != 0 && rb != 0 {
		return ra < rb
	}
	return a < b
}

// loadWorks — оглавление корпуса из frontmatter глав. Неразборная глава в
// оглавление не попадает, как не попадает и в поиск. Ссылки пропускаются:
// handleChapter всё равно не отдал бы ту, что ведёт за corpus/, а в
// оглавлении не должно быть ссылок, которые не открываются.
func loadWorks(dir string) ([]workRef, error) {
	byWork := map[string]*workRef{}
	books := map[string]map[string]*bookRef{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		ch, err := corpus.ParseChapter(rel, data)
		if err != nil {
			return nil
		}
		w, ok := byWork[ch.WorkSlug]
		if !ok {
			w = &workRef{Slug: ch.WorkSlug, Title: ch.Work, Group: ch.Group,
				Authenticity: ch.Authenticity, Translator: ch.Translator}
			byWork[ch.WorkSlug] = w
			books[ch.WorkSlug] = map[string]*bookRef{}
		}
		key := strings.ToLower(ch.Book)
		b, ok := books[ch.WorkSlug][key]
		if !ok {
			b = &bookRef{Book: key}
			books[ch.WorkSlug][key] = b
		}
		b.Chapters = append(b.Chapters, chapterRef{Number: ch.Number, Cite: ch.Cite, Path: rel})
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]workRef, 0, len(byWork))
	for slug, w := range byWork {
		for _, b := range books[slug] {
			sort.Slice(b.Chapters, func(i, j int) bool { return b.Chapters[i].Number < b.Chapters[j].Number })
			w.Books = append(w.Books, *b)
		}
		sort.Slice(w.Books, func(i, j int) bool { return bookLess(w.Books[i].Book, w.Books[j].Book) })
		out = append(out, *w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

// worksIndex читает оглавление один раз: корпус во время работы не меняется.
// Неудача не кэшируется — поправив -root или corpus/, пользователь обновит
// страницу без перезапуска.
func (s *Server) worksIndex() ([]workRef, error) {
	s.worksMu.Lock()
	defer s.worksMu.Unlock()
	if s.works == nil {
		works, err := loadWorks(filepath.Join(s.Root, "corpus"))
		if err != nil {
			return nil, err
		}
		s.works = works
	}
	return s.works, nil
}

func (s *Server) handleWorks(w http.ResponseWriter, r *http.Request) {
	works, err := s.worksIndex()
	if errors.Is(err, fs.ErrNotExist) {
		log.Printf("оглавление корпуса: %v", err)
		http.Error(w, "каталог corpus/ не найден в корне репозитория (параметр -root)", http.StatusInternalServerError)
		return
	}
	if err != nil {
		log.Printf("оглавление корпуса: %v", err)
		http.Error(w, "оглавление корпуса не прочиталось", http.StatusInternalServerError)
		return
	}
	writeJSON(w, works)
}

// chapterPathRe — путь главы внутри corpus/: «работа/глава.md» или
// «работа/книга/глава.md». Сегменты из строчных латинских букв, цифр и
// дефиса, так что «.», «..», слэши в начале и конце, «//», обратные слэши,
// NUL и «%» не пройдут; расширение только .md.
var chapterPathRe = regexp.MustCompile(`^[a-z0-9-]+/(?:[a-z0-9-]+/)?[0-9]+\.md$`)

const (
	// Регулярное выражение не ограничивает длину сегмента, а имя файла длиннее
	// предела файловой системы дало бы ошибку открытия, а не отказ.
	maxChapterPath = 200
	// Столько читает corpus.ParseChapter (буфер сканера); главы корпуса — сотни КБ.
	maxChapterBytes = 4 << 20
)

// errNoChapter — главы по этому пути нет или отдавать её нельзя: файла нет,
// это не обычный файл, или ссылка ведёт за corpus/. Для клиента все три
// случая одинаковы, чтобы по ответам нельзя было разведывать файловую систему.
var errNoChapter = errors.New("нет такой главы")

// readChapter читает файл главы через os.Root: путь разрешается внутри
// corpus/, и ссылка, выводящая за корень, даёт ошибку, а не чужой файл.
// Проверка и чтение идут одним корнем, поэтому подмена файла ссылкой между
// ними тоже не выведет наружу. rel уже прошёл chapterPathRe.
func (s *Server) readChapter(rel string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Join(s.Root, "corpus"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoChapter
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()

	name := filepath.FromSlash(rel)
	fi, err := root.Stat(name) // ссылку внутри корня Stat проходит, наружу — нет
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("глава %s не отдана: %v", rel, err) // в частности, ссылка за corpus/
		}
		return nil, errNoChapter
	}
	// Каталог с именем главы или устройство читать нельзя; FIFO вообще
	// зависло бы на открытии.
	if !fi.Mode().IsRegular() {
		return nil, errNoChapter
	}
	f, err := root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoChapter
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxChapterBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxChapterBytes {
		return nil, errors.New("файл главы слишком велик")
	}
	return data, nil
}

func (s *Server) handleChapter(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if len(rel) > maxChapterPath || !chapterPathRe.MatchString(rel) {
		http.Error(w, "неверный путь главы", http.StatusBadRequest)
		return
	}
	data, err := s.readChapter(rel)
	if errors.Is(err, errNoChapter) {
		http.Error(w, errNoChapter.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("глава %s не прочиталась: %v", rel, err)
		http.Error(w, "глава не прочиталась", http.StatusInternalServerError)
		return
	}
	ch, err := corpus.ParseChapter(rel, data)
	if err != nil {
		log.Printf("глава %s не разобралась: %v", rel, err)
		http.Error(w, "глава повреждена", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"work": ch.Work, "book": ch.Book, "number": ch.Number, "cite": ch.Cite, "group": ch.Group,
		"authenticity": ch.Authenticity, "translator": ch.Translator, "path": ch.Path, "text": ch.Text,
	})
}

func snippet(text string, n int) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

type hitView struct {
	Cite         string `json:"cite"`
	Path         string `json:"path"`
	Work         string `json:"work"`
	Authenticity string `json:"authenticity"`
	Snippet      string `json:"snippet"`
}

const maxQueryRunes = 300

// defaultSearchTimeout — предел одного поиска. Холодный qmd поднимает локальную
// модель десятки секунд, поэтому предел щедрый; но без него зависший qmd
// (скажем, первый запуск, качающий модель на 1,2 ГБ) держал бы запрос,
// горутину и процесс, пока страницу не закроют, а читатель смотрел бы на «ищу…».
const defaultSearchTimeout = 90 * time.Second

func (s *Server) searchDeadline() time.Duration {
	if s.searchTimeout > 0 {
		return s.searchTimeout
	}
	return defaultSearchTimeout
}

// groupRe — имя группы корпуса: строчные латинские слова через дефиса
// («rhetoric-poetics»); начало с дефиса не проходит.
var groupRe = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)

// checkSearchInput отсеивает то, что не должно доходить до qmd. Запрос уходит
// в него позиционным аргументом, и qmd читает начало с «-» как свой флаг:
// «--help» печатает справку, а «-c чужая» подменяет коллекцию. Оболочки на
// пути нет, так что дело только в разборе флагов. Основная защита —
// corpus.Searcher: он ставит запрос последним, после «--». Отказ здесь —
// вторая линия, чтобы такой запрос не доходил до qmd вовсе.
func checkSearchInput(q, group string) string {
	if strings.HasPrefix(q, "-") {
		return "запрос не должен начинаться с «-»"
	}
	if len([]rune(q)) > maxQueryRunes {
		return "слишком длинный запрос"
	}
	if strings.IndexFunc(q, unicode.IsControl) >= 0 {
		return "в запросе недопустимые символы"
	}
	if group != "" && (len(group) > 32 || !groupRe.MatchString(group)) {
		return "неверная группа"
	}
	return ""
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	group := r.URL.Query().Get("group")
	out := []hitView{}
	if q == "" {
		writeJSON(w, out)
		return
	}
	if msg := checkSearchInput(q, group); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	cfg, _, err := s.loadConfig()
	if err != nil {
		// Ошибка называет путь к файлу конфига; читателю он ни к чему.
		log.Printf("поиск по корпусу: %v", err)
		http.Error(w, "конфиг не прочитался", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.searchDeadline())
	defer cancel()
	hits, err := s.finder().Search(ctx, q, group, cfg.CorpusK)
	if err != nil {
		if r.Context().Err() != nil {
			return // читатель ушёл со страницы; отвечать некому, ошибкой это не считается
		}
		// Срок смотрим по контексту, а не по тексту ошибки: убитый по сроку
		// процесс отдаёт «signal: killed», а не context.DeadlineExceeded.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("поиск по корпусу не уложился в %v", s.searchDeadline())
			// 504, а не 502: qmd не ответил ошибкой, а промолчал до срока.
			http.Error(w, "поиск не уложился во время", http.StatusGatewayTimeout)
			return
		}
		// Подробности (путь корня, вывод qmd) — в журнал сервера, а не читателю.
		log.Printf("поиск по корпусу: %v", err)
		msg := "поиск не удался: ошибка qmd или не построен индекс (make index); подробности в журнале сервера"
		if errors.Is(err, exec.ErrNotFound) {
			msg = "поиск недоступен: программа qmd не найдена в PATH"
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}
	for _, h := range hits {
		c := h.Chapter
		out = append(out, hitView{Cite: c.Cite, Path: c.Path, Work: c.Work,
			Authenticity: c.Authenticity, Snippet: snippet(c.Text, 280)})
	}
	writeJSON(w, out)
}
