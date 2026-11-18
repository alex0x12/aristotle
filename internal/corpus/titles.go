package corpus

import (
	"regexp"
	"strings"
)

// WorkTitle — название сочинения для показа: slug каталога корпуса, английское
// название и русское. En повторяет поле work из frontmatter глав (тест это
// сверяет с файлами): ссылка вида «<En> <нумерация>» переводится без диска.
type WorkTitle struct {
	Slug, En, Ru string
}

// titles — таблица русских названий. Она только для показа: ссылки в журнале,
// разметка модели и сверка (render.VerifyInChapter сравнивает cite строго)
// остаются английскими, а русское название на экране и в выгрузке берётся
// отсюда, из одного места.
//
// Названия — по изданию «Аристотель. Сочинения в четырёх томах» (М.: Мысль),
// где работа в него входит; иначе — по названию русского перевода. Год
// проверен только для тома 2 (1978, перечень nsc.ru), остальных не проверяли.
// Работы, которых нет в таблице, показываются по-английски, ошибкой это не
// считается.
//
// Где перечни расходятся, взят вариант опубликованных переводчиков — решение
// автора; над такой строкой сказано, на каком источнике оно держится, чтобы спор
// не открывался заново. Составное название «On Youth and Old Age, …» оставлено
// одним, как в корпусе.
//
// Как проверялось (18.11.26, только по страницам в сети, через пересказ
// страниц; ни одна книга с полки не открывалась): «Мысль т.N» — оглавление
// тома N (т.1 и т.3 — на iphlib.ru; т.2 — по перечню переводов nsc.ru и
// объявлению о продаже; т.4 — неполная страница LiveLib); «списки» — русские
// перечни сочинений (ru.wikipedia «Аристотель», Викитека, wikilivres.ru,
// aristoteles.philosophy.nsc.ru/translations, «Список русских переводов
// сочинений Аристотеля» А. Орлова в Respublica Literaria), которые сходятся
// на этом названии.
var titles = []WorkTitle{
	// «Афинская полития» — во всех перечнях (Орлов, nsc.ru, ru.wikipedia, Викитека,
	// wikilivres). Орлов называет перевод С. И. Радцига (1936/37/97). Входит ли в
	// «Мысль» т.4 — не установлено.
	{"athenian-constitution", "The Athenian Constitution", "Афинская полития"}, // списки
	{"categories", "Categories", "Категории"},                                  // Мысль т.2 (Органон) + списки
	// Выбран вариант опубликованных переводчиков; решение автора. «О передвижении
	// животных» — пер. Е. В. Афонасина, 2017; Орлов, Викитека, wikilivres. «О способах
	// передвижения животных» (ru.wikipedia, nsc.ru) не взято.
	{"gait-of-animals", "On the Gait of Animals", "О передвижении животных"},
	{"generation-and-corruption", "On Generation and Corruption", "О возникновении и уничтожении"}, // Мысль т.3 + списки
	{"generation-of-animals", "On the Generation of Animals", "О возникновении животных"},          // списки (В. П. Карпов, 1940)
	{"history-of-animals", "The History of Animals", "История животных"},                           // списки (В. П. Карпов)
	// «О долготе и краткости жизни» — единодушно: Орлов, nsc.ru, ru.wikipedia,
	// Викитека, статья Солоповой (пер. М. А. Солоповой, 2016).
	{"longevity-and-shortness-of-life", "On Longevity and Shortness of Life", "О долготе и краткости жизни"},
	{"metaphysics", "Metaphysics", "Метафизика"},                               // Мысль т.1 + списки
	{"meteorology", "Meteorology", "Метеорологика"},                            // Мысль т.3 + списки
	{"movement-of-animals", "On the Motion of Animals", "О движении животных"}, // списки (Е. В. Афонасин, 2016)
	{"nicomachean-ethics", "Nicomachean Ethics", "Никомахова этика"},           // Мысль т.4 (Н. В. Брагинская) + списки
	// Выбран вариант опубликованных переводчиков; решение автора. «О сновидениях» —
	// пер. О. А. Чулкова (2005; Орлов), Месяц (2024; nsc.ru). ru.wikipedia, Викитека,
	// wikilivres пишут «О сновидении» (ед. число).
	{"on-dreams", "On Dreams", "О сновидениях"},
	{"on-interpretation", "On Interpretation", "Об истолковании"}, // Мысль т.2 (Органон) + списки
	// Выбран вариант опубликованных переводчиков; решение автора. «О памяти и
	// припоминании» — в переводах (С. В. Месяц, Е. В. Алымова; Орлов, nsc.ru).
	// «О памяти и воспоминании» (ru.wikipedia, Викитека, wikilivres) не взято.
	{"on-memory", "On Memory and Reminiscence", "О памяти и припоминании"},
	// Выбран вариант опубликованных переводчиков; решение автора. «О предсказаниях
	// во сне» (пер. М. А. Солоповой, 2010; Орлов, nsc.ru) — единственный полный
	// русский перевод. ru.wikipedia и Викитека — «О толковании сновидений»; не взято.
	{"on-prophesying-by-dreams", "On Prophesying by Dreams", "О предсказаниях во сне"},
	// Выбран вариант опубликованных переводчиков; решение автора. «О чувственном
	// восприятии» — пер. Е. В. Алымовой (2004), Месяц (2015); Орлов. ru.wikipedia,
	// Викитека, wikilivres — «О восприятии и воспринимаемом» (De sensu et
	// sensibilibus); не взято.
	{"on-sense-and-the-sensible", "On Sense and the Sensible", "О чувственном восприятии"},
	{"on-sleep", "On Sleep and Sleeplessness", "О сне и бодрствовании"}, // списки
	{"on-the-heavens", "On the Heavens", "О небе"},                      // Мысль т.3 (А. В. Лебедев) + списки
	{"on-the-soul", "On the Soul", "О душе"},                            // Мысль т.1 (П. С. Попов) + списки
	// Решение автора: составное название оставлено одним, как в корпусе (одна
	// работа, один каталог). Единого русского названия у составного заголовка нет;
	// формулировка взята по ru.wikipedia. У Солоповой (2020) — «О юности и старости.
	// О жизни и смерти», «О дыхании» отдельно.
	{"on-youth-and-old-age", "On Youth and Old Age, On Life and Death, On Breathing",
		"О юности и старости, о жизни и смерти и о дыхании"},
	{"parts-of-animals", "On the Parts of Animals", "О частях животных"},                       // списки (В. П. Карпов, 1937)
	{"physics", "Physics", "Физика"},                                                           // Мысль т.3 (В. П. Карпов) + списки
	{"poetics", "Poetics", "Поэтика"},                                                          // списки (М. Л. Гаспаров)
	{"politics", "Politics", "Политика"},                                                       // Мысль т.4 (С. А. Жебелёв) + списки
	{"posterior-analytics", "Posterior Analytics", "Вторая аналитика"},                         // Мысль т.2 («Аналитики первая и вторая») + списки
	{"prior-analytics", "Prior Analytics", "Первая аналитика"},                                 // Мысль т.2 («Аналитики первая и вторая») + списки
	{"rhetoric", "Rhetoric", "Риторика"},                                                       // списки
	{"sophistical-refutations", "On Sophistical Refutations", "О софистических опровержениях"}, // Мысль т.2 (Органон) + списки
	{"topics", "Topics", "Топика"},                                                             // Мысль т.2 (Органон) + списки
}

func titleBySlug(slug string) (WorkTitle, bool) {
	for _, w := range titles {
		if w.Slug == slug {
			return w, true
		}
	}
	return WorkTitle{}, false
}

// TitleRu — русское название работы по slug её каталога. Работы нет в таблице —
// ("", false), и показывать надо английское.
func TitleRu(slug string) (string, bool) {
	w, ok := titleBySlug(slug)
	return w.Ru, ok
}

// WorkRu — русское название по английскому (поле work из frontmatter);
// сравнение точное, с учётом регистра.
func WorkRu(en string) (string, bool) {
	if en == "" {
		return "", false
	}
	for _, w := range titles {
		if w.En == en {
			return w.Ru, true
		}
	}
	return "", false
}

// CiteRu — русская форма ссылки: «Nicomachean Ethics II.1» → «Никомахова этика
// II.1». Нумерация остаётся как есть. Не нашлась работа — ("", false).
func CiteRu(cite string) (string, bool) { return citeRu(titles, cite) }

// SplitCiteRu — та же ссылка по частям: русское название и нумерация. Нужна
// показу, который ставит между ними точку: делить готовую строку по пробелу
// значило бы гадать, а нумерация у модели бывает и «II.1, II.6».
func SplitCiteRu(cite string) (ru, numbering string, ok bool) { return splitCite(titles, cite) }

func citeRu(table []WorkTitle, cite string) (string, bool) {
	ru, num, ok := splitCite(table, cite)
	if !ok {
		return "", false
	}
	return ru + " " + num, true
}

// numberingRe — начало нумерации так, как её пишет корпус: «книга.глава»
// («II.1») или просто глава («6»); дальше может идти что угодно («II.1, II.6»).
var numberingRe = regexp.MustCompile(`^(?:[IVXLCDM]+\.)?[0-9]+`)

// splitCite ищет запись, чьё английское название — самый длинный префикс ссылки
// с одним пробелом после него. Самый длинный — чтобы «On Sleep» не отобрал
// «On Sleep and Sleeplessness 2» у настоящей работы. Всё строго: регистр
// значим, пробелы не срезаются. Остаток обязан начинаться с нумерации, иначе
// название без номера («On Sleep and Sleeplessness») совпало бы с более
// коротким («On Sleep»), а «Politics Book II» стало бы «Политика Book II».
func splitCite(table []WorkTitle, cite string) (ru, numbering string, ok bool) {
	best := -1
	for i, w := range table {
		if w.En == "" || !strings.HasPrefix(cite, w.En+" ") {
			continue
		}
		if !numberingRe.MatchString(cite[len(w.En)+1:]) {
			continue
		}
		if best < 0 || len(w.En) > len(table[best].En) {
			best = i
		}
	}
	if best < 0 {
		return "", "", false
	}
	return table[best].Ru, cite[len(table[best].En)+1:], true
}
