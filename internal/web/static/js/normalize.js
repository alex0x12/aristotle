// Та же нормализация, что render.Normalize на сервере: нижний регистр, буквы
// и цифры как есть, всё прочее — один пробел. Плюс карта: для каждой единицы
// UTF-16 нормализованной строки — индекс символа в исходном тексте, чтобы
// найденное в нормализованном виде подсветить в настоящем.
// Как unicode.IsLetter || unicode.IsDigit в Go: IsDigit — только десятичные
// цифры (Nd), поэтому дроби, степени и римские цифры (No, Nl) — не слово, а
// разделитель; с \p{N} нормализация расходилась бы с серверной.
const WORD = /[\p{L}\p{Nd}]/u;

export function normalizeWithMap(text) {
  let norm = '';
  const map = [];
  let prevSpace = true;
  for (let i = 0; i < text.length;) {
    const ch = String.fromCodePoint(text.codePointAt(i));
    if (WORD.test(ch)) {
      const low = ch.toLowerCase();
      for (let k = 0; k < low.length; k++) map.push(i);
      norm += low;
      prevSpace = false;
    } else if (!prevSpace) {
      norm += ' ';
      map.push(i);
      prevSpace = true;
    }
    i += ch.length;
  }
  if (norm.endsWith(' ')) { norm = norm.slice(0, -1); map.pop(); }
  return { norm, map };
}

function charEnd(text, i) { return i + String.fromCodePoint(text.codePointAt(i)).length; }

// findExcerpt — диапазоны [начало, конец) выдержки в тексте главы. Сначала
// целиком; не нашлась — кусками между многоточиями (элизия), каждый не
// короче трёх слов, по порядку. Не нашлось — пустой список: тогда панель
// показывает выдержку над главой без подсветки (спека 2.7).
export function findExcerpt(chapter, en) {
  const { norm, map } = normalizeWithMap(chapter);
  const locate = (q, from) => {
    const at = norm.indexOf(q, from);
    if (at < 0) return null;
    return { end: at + q.length, range: [map[at], charEnd(chapter, map[at + q.length - 1])] };
  };
  const whole = normalizeWithMap(en).norm;
  if (!whole) return [];
  const hit = locate(whole, 0);
  if (hit) return [hit.range];
  const parts = en.split(/\s*(?:…|\.\.\.)\s*/)
    .map(p => normalizeWithMap(p).norm)
    .filter(p => p.split(' ').length >= 3);
  if (parts.length < 2) return [];
  const ranges = [];
  let from = 0;
  for (const p of parts) {
    const h = locate(p, from);
    if (!h) return [];
    ranges.push(h.range);
    from = h.end;
  }
  return ranges;
}
