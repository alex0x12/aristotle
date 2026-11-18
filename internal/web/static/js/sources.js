import { get, esc } from './api.js';
import { findExcerpt } from './normalize.js';

// paragraphs — текст главы абзацами, с <mark> на найденных диапазонах.
// Абзацы режутся до подсветки, чтобы <mark> не пересекал границу <p>.
export function paragraphs(text, ranges = []) {
  const parts = [];
  const re = /\n\s*\n/g;
  let start = 0;
  let m;
  while ((m = re.exec(text))) { parts.push([start, m.index]); start = m.index + m[0].length; }
  parts.push([start, text.length]);
  return parts.map(([a, b]) => {
    let html = '';
    let pos = a;
    for (const [s, e] of ranges) {
      const s2 = Math.max(s, a), e2 = Math.min(e, b);
      if (s2 >= e2) continue;
      html += esc(text.slice(pos, s2)) + '<mark>' + esc(text.slice(s2, e2)) + '</mark>';
      pos = e2;
    }
    html += esc(text.slice(pos, b));
    return html.trim() ? `<p>${html}</p>` : '';
  }).join('');
}

// openSource — панель источников. Блок с unknown — врезка из журнала
// командной строки: глава того хода не сохранена, и «нет в выдержках» про неё
// сказать нельзя.
export async function openSource(b) {
  const panel = document.getElementById('src');
  const box = document.getElementById('src-in');
  panel.hidden = false;
  const state = b.unknown ? ['', 'СВЕРКА НЕ СОХРАНЕНА']
    : b.verified ? ['ok', 'СВЕРЕНО С ГЛАВОЙ'] : ['bad', 'НЕТ В ВЫДЕРЖКАХ'];
  box.innerHTML = `<button class="close" title="Закрыть" type="button">×</button>
    <h5>ИСТОЧНИК</h5><h6>${esc(b.cite || 'без ссылки')}</h6>
    ${b.ru ? `<p class="ru">${esc(b.ru)}</p>` : ''}
    <p class="state ${state[0]}">${state[1]}</p>
    <div class="chapter">загружаю главу…</div>`;
  box.querySelector('.close').onclick = () => { panel.hidden = true; };
  const holder = box.querySelector('.chapter');
  if (!b.chapter) {
    holder.innerHTML = `<p class="en">${esc(b.en)}</p><p class="hint">${b.unknown
      ? 'Глава этого хода в журнале не сохранена.'
      : 'Главы с такой ссылкой в выдаче поиска не было.'}</p>`;
    return;
  }
  try {
    const ch = await get('/api/corpus/chapter?path=' + encodeURIComponent(b.chapter));
    const chText = ch.text ?? ''; // поля с omitempty на проводе бывают без ключа
    const ranges = findExcerpt(chText, b.en || '');
    holder.innerHTML = (ranges.length ? '' : `<p class="en">${esc(b.en)}</p>`) +
      paragraphs(chText, ranges) +
      `<p class="lib-link"><a href="#/library/${encodeURI(b.chapter)}">ЧИТАТЬ В БИБЛИОТЕКЕ</a></p>`;
    holder.querySelector('mark')?.scrollIntoView({ block: 'center' });
  } catch (e) {
    holder.textContent = 'глава не открылась: ' + e.message;
  }
}
