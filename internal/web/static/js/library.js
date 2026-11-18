import { get, esc } from './api.js';
import { paragraphs } from './sources.js';

const GROUPS = {
  logic: 'ЛОГИКА', physics: 'ФИЗИКА', psychology: 'ПСИХОЛОГИЯ', biology: 'БИОЛОГИЯ',
  metaphysics: 'МЕТАФИЗИКА', ethics: 'ЭТИКА', politics: 'ПОЛИТИКА', 'rhetoric-poetics': 'РИТОРИКА И ПОЭТИКА',
};
const AUTH = { spurious: 'подложное', disputed: 'подлинность оспаривается' };

// Метка у всего, что не «genuine»: сервер присылает подлинность как есть из
// frontmatter, и значение, которого здесь не знают, всё равно не подлинное.
function badge(a) {
  a ??= '';
  if (!a || a === 'genuine') return '';
  return `<span class="badge">${esc(AUTH[a] ?? 'подлинность: ' + a)}</span>`;
}

// Упавший fetch (сервер остановлен) даёт английский TypeError; ошибку
// сервера api.js отдаёт его же текстом, он уже по-русски.
function reason(e) {
  return e instanceof TypeError ? 'нет связи с сервером' : e.message;
}

const groupName = g => GROUPS[g] ?? (g ? g.toUpperCase() : 'ПРОЧЕЕ');

// Смена адреса #/library/<глава> вызывает showLibrary заново, но перерисовка
// сбросила бы результаты поиска, раскрытые работы и прокрутку списка. Пока
// экран ещё в центре, он остаётся, а меняется только то, что справа.
let live = null;

export async function showLibrary(center, path) {
  if (live && center.contains(live.root)) return live.show(path);

  center.innerHTML = `<div class="library">
    <div class="mosaic"></div>
    <form class="search" id="lib-search">
      <input id="lib-q" maxlength="300" autocomplete="off" placeholder="Искать в корпусе (по-английски: virtue, habit, the mean)">
      <button class="btn" type="submit">ИСКАТЬ</button>
    </form>
    <div class="cols"><nav class="works" id="works">загружаю оглавление…</nav><div class="reader" id="reader"></div></div>
  </div>`;
  const root = center.querySelector('.library');
  const worksBox = root.querySelector('#works');
  const reader = root.querySelector('#reader');
  const form = root.querySelector('#lib-search');
  const input = root.querySelector('#lib-q');
  const submit = form.querySelector('button');

  let cur = '';   // открытая глава
  let found = null; // последний удачный поиск: { q, hits }
  let seq = 0;    // ответ устаревшего запроса не должен затирать новый

  // mark — подсветить главу в оглавлении и раскрыть её работу.
  const mark = p => {
    worksBox.querySelectorAll('.chapters a.on').forEach(a => a.classList.remove('on'));
    if (!p) return;
    const a = worksBox.querySelector(`.chapters a[data-path="${CSS.escape(p)}"]`);
    if (!a) return;
    a.classList.add('on');
    const list = a.closest('.chapters');
    list.hidden = false;
    list.previousElementSibling.setAttribute('aria-expanded', 'true');
    a.scrollIntoView({ block: 'nearest' });
  };

  const hint = text => { reader.innerHTML = `<p class="hint">${esc(text)}</p>`; };
  const fail = text => { reader.innerHTML = `<p class="banner">${esc(text)}</p>`; };

  const showHits = ({ q, hits }) => {
    reader.innerHTML = hits.length
      ? `<p class="hint">найдено: ${hits.length} · «${esc(q)}»</p>` + hits.map(h =>
        `<a class="hit" data-path="${esc(h.path)}" href="#/library/${esc(encodeURI(h.path ?? ''))}"><b>${esc(h.cite)}</b>${badge(h.authenticity)}<span class="snip">${esc(h.snippet)}</span></a>`).join('')
      : `<p class="hint">по запросу «${esc(q)}» ничего не найдено</p>`;
    reader.scrollTop = 0;
  };

  const open = async p => {
    const my = ++seq;
    mark(p);
    hint('загружаю главу…');
    try {
      const ch = await get('/api/corpus/chapter?path=' + encodeURIComponent(p));
      if (my !== seq) return;
      // translator и book у сервера бывают пустыми; text читаем так же, как sources.js.
      reader.innerHTML = `<h2>${esc(ch.cite)}</h2>
        <div class="sub">${esc(ch.work)}${ch.translator ? ' · перевод ' + esc(ch.translator) : ''}${badge(ch.authenticity)}</div>
        ${paragraphs(ch.text ?? '')}`;
      reader.scrollTop = 0;
    } catch (e) {
      if (my === seq) fail('глава не открылась: ' + reason(e));
    }
  };

  // show — то, что справа, по адресу: глава; а без главы — последний поиск
  // (сюда возвращает кнопка «назад» из главы, открытой из выдачи) или подсказка.
  const show = p => {
    cur = p ?? '';
    if (cur) return open(cur);
    ++seq;
    mark('');
    if (found) showHits(found); else hint('Выберите сочинение слева или найдите место поиском.');
  };
  live = { root, show };

  form.addEventListener('submit', async e => {
    e.preventDefault();
    const q = input.value.trim();
    if (!q) return;
    const my = ++seq;
    submit.disabled = true;
    hint(`ищу «${q}»… холодный поиск поднимается до 15 секунд`);
    try {
      const hits = await get('/api/corpus/search?q=' + encodeURIComponent(q));
      if (my !== seq) return;
      found = { q, hits: hits ?? [] };
      showHits(found);
    } catch (err) {
      if (my === seq) fail(reason(err));
    } finally {
      submit.disabled = false;
    }
  });

  worksBox.addEventListener('click', e => {
    const b = e.target.closest('.work > button');
    if (!b) return;
    const list = b.nextElementSibling;
    list.hidden = !list.hidden;
    b.setAttribute('aria-expanded', String(!list.hidden));
  });

  // Ссылка на уже открытую главу адреса не меняет, и hashchange не придёт; а справа
  // тем временем могла остаться выдача поиска. Открыть главу заново самим.
  root.addEventListener('click', e => {
    const a = e.target.closest('a[data-path]');
    if (!a || a.dataset.path !== cur || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey) return;
    e.preventDefault();
    show(cur);
  });

  show(path);

  try {
    const works = (await get('/api/corpus/works')) ?? [];
    const byGroup = {};
    for (const w of works) (byGroup[w.group ?? ''] ??= []).push(w);
    // Известные группы — в порядке корпуса, неизвестные (новые в корпусе) — следом.
    const order = [...Object.keys(GROUPS), ...Object.keys(byGroup).filter(g => !(g in GROUPS))];
    worksBox.innerHTML = order.filter(g => byGroup[g]).map(g =>
      `<h4>${esc(groupName(g))}</h4>` + byGroup[g].map(w => `<div class="work">
        <button type="button" aria-expanded="false">${esc(w.title ?? w.slug)}${badge(w.authenticity)}</button>
        <div class="chapters" hidden>${(w.books ?? []).map(b =>
          ((b.book ?? '') ? `<span class="book">КНИГА ${esc(b.book.toUpperCase())}</span>` : '') +
          (b.chapters ?? []).map(c =>
            `<a href="#/library/${esc(encodeURI(c.path ?? ''))}" data-path="${esc(c.path)}" title="${esc(c.cite)}">${esc(c.number)}</a>`).join('')
        ).join('')}</div></div>`).join('')).join('');
    mark(cur);
  } catch (e) {
    worksBox.innerHTML = `<p class="status">оглавление не загрузилось: ${esc(reason(e))}</p>`;
  }
}
