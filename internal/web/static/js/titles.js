import { esc } from './api.js';

// Русские названия работ приходят с сервера (cite_ru, work_ru, title_ru) и
// бывают без ключа: у работы вне таблицы корпуса их нет (omitempty). Во всём
// здесь русское читается через ?? '', при его отсутствии показывается
// английское, и каждая строка проходит esc().

// numbering — нумерация ссылки: остаток после названия работы и пробела
// («II.1» из «Nicomachean Ethics II.1»). Ссылки так и устроены; на том же
// правиле стоит перевод на сервере (corpus.CiteRu). Чужой префикс — нумерации нет.
export function numbering(cite, work) {
  cite ??= '';
  work ??= '';
  return work && cite.startsWith(work + ' ') ? cite.slice(work.length + 1) : '';
}

// join — «Название · нумерация»; регистр заглавных дело CSS, а не разметки.
const join = (title, num) => `${esc(title)} · ${esc(num)}`;

// quoteHead — заголовок ссылки врезки: html — что показать, en — английская
// ссылка второй строкой (пусто, когда html и есть английская).
export function quoteHead(b) {
  const full = b.cite_ru ?? '';
  const title = b.work_ru ?? '';
  if (!full) return { html: esc(b.cite || 'без ссылки'), en: '' };
  const num = numbering(full, title);
  return { html: title && num ? join(title, num) : esc(full), en: esc(b.cite ?? '') };
}

// chapterHead — то же для главы из библиотеки: у неё работа и ссылка приходят
// отдельно, так что нумерация берётся из английской ссылки по английской работе.
// Нет русского названия или нумерацию отделить не удалось — ссылка как есть, по-английски:
// потерять номер главы хуже, чем показать её без перевода.
export function chapterHead(workRu, work, cite) {
  const title = workRu ?? '';
  const num = numbering(cite, work);
  if (!title || !num) return { html: esc(cite ?? ''), en: '' };
  return { html: join(title, num), en: esc(cite) };
}
