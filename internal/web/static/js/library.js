import { get, esc } from './api.js';
import { paragraphs } from './sources.js';
import { chapterHead } from './titles.js';

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

// shown — название работы в списке: русское, а нет его (title_ru без ключа) — английское.
const shown = w => w.title_ru || w.title || w.slug || '';

const groupName = g => GROUPS[g] ?? (g ? g.toUpperCase() : 'ПРОЧЕЕ');

// Смена адреса #/library/<глава> вызывает showLibrary заново, но перерисовка
// сбросила бы результаты поиска, раскрытые работы и прокрутку списка. Пока
// экран ещё в центре, он остаётся, а меняется только то, что справа.
let live = null;

export async function showLibrary(center, path) {
  if (live && center.contains(live.root)) return live.show(path);
  live?.cancel(); // прежний экран ушёл из центра: его запрос больше никому не нужен

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
  let ctl = null; // обрыв запроса, который идёт сейчас

  // begin — начало нового запроса справа. Прежний, если ещё идёт, обрывается, и
  // кнопка поиска отпускается тут же: её включение не должно зависеть от того,
  // придёт ли когда-нибудь ответ на запрос, который уже никому не нужен.
  const begin = () => {
    ctl?.abort();
    ctl = new AbortController();
    submit.disabled = false;
    return { my: ++seq, signal: ctl.signal };
  };

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
      ? `<p class="hint">найдено: ${hits.length} · «${esc(q)}»</p>` + hits.map(h => {
        const head = chapterHead(h.work_ru, h.work, h.cite);
        return `<a class="hit" data-path="${esc(h.path)}" href="#/library/${esc(encodeURI(h.path ?? ''))}"><b>${head.html}</b>${head.en ? `<small>${head.en}</small>` : ''}${badge(h.authenticity)}<span class="snip">${esc(h.snippet)}</span></a>`;
      }).join('')
      : `<p class="hint">по запросу «${esc(q)}» ничего не найдено</p>`;
    reader.scrollTop = 0;
  };

  const open = async p => {
    const { my, signal } = begin();
    mark(p);
    hint('загружаю главу…');
    try {
      const ch = await get('/api/corpus/chapter?path=' + encodeURIComponent(p), signal);
      if (my !== seq) return;
      // translator и book у сервера бывают пустыми; text читаем так же, как sources.js.
      // work_ru бывает без ключа: тогда заголовок и подзаголовок прежние, английские.
      const head = chapterHead(ch.work_ru, ch.work, ch.cite);
      reader.innerHTML = `<h2>${head.html}</h2>
        <div class="sub">${head.en || esc(ch.work)}${ch.translator ? ' · перевод ' + esc(ch.translator) : ''}${badge(ch.authenticity)}</div>
        ${paragraphs(ch.text ?? '')}`;
      reader.scrollTop = 0;
    } catch (e) {
      if (e.name === 'AbortError') return;
      if (my === seq) fail('глава не открылась: ' + reason(e));
    }
  };

  // show — то, что справа, по адресу: глава; а без главы — последний поиск
  // (сюда возвращает кнопка «назад» из главы, открытой из выдачи) или подсказка.
  const show = p => {
    cur = p ?? '';
    if (cur) return open(cur);
    begin();
    mark('');
    if (found) showHits(found); else hint('Выберите сочинение слева или найдите место поиском.');
  };
  live = { root, show, cancel: () => ctl?.abort() };

  form.addEventListener('submit', async e => {
    e.preventDefault();
    const q = input.value.trim();
    if (!q) return;
    const { my, signal } = begin();
    submit.disabled = true;
    hint(`ищу «${q}»… холодный поиск поднимается до 15 секунд`);
    try {
      const hits = await get('/api/corpus/search?q=' + encodeURIComponent(q), signal);
      if (my !== seq) return;
      found = { q, hits: hits ?? [] };
      showHits(found);
    } catch (err) {
      if (err.name === 'AbortError') return;
      if (my === seq) fail(reason(err));
    } finally {
      // Только свой запрос: вытесненный уже отпущен в begin(), а включить кнопку
      // здесь значило бы разблокировать её посреди чужого поиска.
      if (my === seq) submit.disabled = false;
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
    // Сервер сортирует по английскому названию, а читатель ищет по русскому.
    for (const list of Object.values(byGroup)) list.sort((a, b) => shown(a).localeCompare(shown(b), 'ru'));
    // Известные группы — в порядке корпуса, неизвестные (новые в корпусе) — следом.
    const order = [...Object.keys(GROUPS), ...Object.keys(byGroup).filter(g => !(g in GROUPS))];
    worksBox.innerHTML = order.filter(g => byGroup[g]).map(g =>
      `<h4>${esc(groupName(g))}</h4>` + byGroup[g].map(w => `<div class="work">
        <button type="button" aria-expanded="false"><span class="wt">${esc(shown(w))}${w.title_ru ? `<small>${esc(w.title)}</small>` : ''}</span>${badge(w.authenticity)}</button>
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
