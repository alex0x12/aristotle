package web

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/alex0x12/aristotle/internal/chat"
	"github.com/alex0x12/aristotle/internal/render"
)

// oneLine — строка в одну линию: перевод строки в названии сломал бы заголовок
// Markdown и, в правленом вручную журнале, мог бы увести дальше.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// quoted — текст врезки как цитата Markdown: каждая строка со своим «>».
func quoted(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\n", "\n> ") }

// citeLine — адрес врезки для выгрузки: русское название работы и английская
// ссылка в скобках рядом («Никомахова этика II.1 (Nicomachean Ethics II.1)»).
// Работа, которой нет в таблице названий, остаётся английской, без скобок.
func citeLine(bl blockView) string {
	if bl.CiteRu != "" && bl.CiteRu != bl.Cite {
		return fmt.Sprintf("%s (%s)", bl.CiteRu, bl.Cite)
	}
	return bl.Cite
}

// exportMarkdown — беседа для чтения вне интерфейса: вопросы, проза, врезки
// с адресом и пометкой сверки, английский оригинал курсивом, пометки.
func exportMarkdown(title string, recs []chat.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", oneLine(title))
	for _, it := range items(recs) {
		switch it.Type {
		case "user":
			fmt.Fprintf(&b, "**Вопрос.** %s\n\n", it.Text)
		case "assistant":
			for _, bl := range it.Blocks {
				switch bl.Kind {
				case render.BlockProse:
					if strings.TrimSpace(bl.Text) != "" {
						b.WriteString(bl.Text + "\n\n")
					}
				case render.BlockQuote:
					text := bl.Ru
					if text == "" {
						text = bl.En
					}
					fmt.Fprintf(&b, "> %s\n>\n> — %s", quoted(text), citeLine(bl))
					switch {
					case it.QuotesUnknown:
						b.WriteString(" *(сверка не сохранена)*")
					case !bl.Verified:
						b.WriteString(" *(нет в выдержках)*")
					}
					b.WriteString("\n")
					if bl.En != "" && bl.Ru != "" {
						fmt.Fprintf(&b, ">\n> *%s*\n", quoted(bl.En))
					}
					b.WriteString("\n")
				default:
					fmt.Fprintf(&b, "**[%s]** %s\n\n", bl.Kind, bl.Text)
				}
			}
			if it.StopReason == "cancelled" {
				b.WriteString("*(ответ остановлен)*\n\n")
			}
			b.WriteString("---\n\n")
		}
	}
	return b.String()
}

// maxExportName — длина названия в имени файла, знаков: имя длиннее ни одна
// файловая система не любит, а название беседы бывает и абзацем.
const maxExportName = 80

// exportFileName — имя файла из названия беседы. Названию верить нельзя (его
// пишет пользователь, журнал правят вручную): остаются буквы, цифры и
// скромная пунктуация, пробелы сжаты, косые черты, кавычки и двоеточия
// уходят, длина ограничена, а название из одних точек и пробелов — пусто.
func exportFileName(title string) string {
	var b strings.Builder
	for _, r := range oneLine(title) {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == ':' || r == ';' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|':
		case unicode.IsControl(r) || !unicode.IsPrint(r):
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	name := []rune(strings.Join(strings.Fields(b.String()), " "))
	if len(name) > maxExportName {
		name = name[:maxExportName]
	}
	// «..» — путь вверх, точка в начале — скрытый файл, а на конце точку и пробел
	// Windows не терпит. Свёртка «..» повторяется: из «....» один проход
	// оставил бы снова «..».
	out := string(name)
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", ".")
	}
	return strings.Trim(out, ". ")
}

// attrEncode — значение для filename* по RFC 8187: всё, кроме attr-char,
// процентами по байтам UTF-8.
func attrEncode(s string) string {
	const attrChar = "!#$&+-.^_`|~"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(attrChar, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// contentDisposition — вложение с именем из названия беседы. filename* несёт
// русское имя (RFC 8187), filename — ASCII-запасное из id беседы для тех, кто
// filename* не понимает; id — только цифры, «T» и дефисы (idRe).
func contentDisposition(id, title string) string {
	fallback := "aristotle-" + id + ".md"
	name := exportFileName(title)
	if name == "" {
		name = "aristotle-" + id
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fallback, attrEncode(name+".md"))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	c, ok := s.convFromPath(w, r)
	if !ok {
		return
	}
	recs, err := c.history.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	title := summarize(c.id, recs).Title
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", contentDisposition(c.id, title))
	fmt.Fprint(w, exportMarkdown(title, recs))
}
