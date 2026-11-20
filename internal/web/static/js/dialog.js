import { get, esc, usageText, usageIsPartial, getLang, setLang, langParam } from './api.js';
import { readSSE } from './stream.js';
import { openSource } from './sources.js';
import { quoteHead } from './titles.js';

const EXAMPLES = [
  'Что такое добродетель?', 'В чём счастье человека?',
  'Можно ли дружить с тем, кто тебе неприятен?', 'Почему люди поступают дурно, зная, что это дурно?',
  'Какое государственное устройство лучшее?', 'Что такое душа?',
];
const NOTE = { 'вывод': 'ВЫВОД', 'факт': 'ФАКТ', 'посылка': 'ПОСЫЛКА' };
const TRIM_TURNS = 'раньше этого места философ не видит';
const TRIM_EXCERPTS = 'выдержки прошлых ходов опущены';

function el(html) {
  const t = document.createElement('template');
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

// blockEl — блок ответа. Врезка открывает главу в панели источников.
// Пустая проза — разрыв абзаца: отдельного элемента ей не нужно, отступ
// между абзацами уже есть.
export function blockEl(b, quotesUnknown = false) {
  // Пустая строка приходит блоком без ключа text (omitempty на сервере): без ?? '' это TypeError.
  if (b.kind === 'prose') return (b.text ?? '').trim() ? el(`<p>${esc(b.text)}</p>`) : null;
  if (b.kind === 'quote') {
    const mark = quotesUnknown ? '<span class="unk">сверка не сохранена</span>'
      : b.verified ? '' : '<span class="warn">нет в выдержках</span>';
    // Ссылка по-русски («Никомахова этика · II.1»), если сервер знает название работы;
    // cite_ru и work_ru бывают без ключа. Английскую ссылку покажет панель источников.
    const q = el(`<blockquote class="quote${b.verified || quotesUnknown ? '' : ' bad'}" title="Открыть главу">
      ${esc(b.ru || b.en)}<cite>${quoteHead(b).html}${mark}</cite></blockquote>`);
    q.addEventListener('click', () => openSource(quotesUnknown ? { ...b, unknown: true } : b));
    return q;
  }
  return el(`<p class="note"><span class="tag">${NOTE[b.kind] ?? esc(b.kind)}</span> ${esc(b.text)}</p>`);
}

function metaLine(usage) {
  usage ??= {};
  const line = el(`<div class="meta-line">${esc(usageText(usage))}</div>`);
  if (!usage.known) {
    line.title = usageIsPartial(usage)
      ? 'Сумма завершённых запросов: ответ не дошёл до конца, итог провайдер не сообщил'
      : 'Провайдер расход не сообщил';
  }
  return line;
}

function turnEl(question) {
  const t = el('<section class="turn"></section>');
  t.append(el(`<div class="q">${esc(question)}</div>`));
  return t;
}

// markTrim — ходы со второго по turns+1 модель уже не видит; первый — видит.
// Граница стоит перед первым ходом, который она видит снова. Если урезаны
// только выдержки, ходы целы, а выдержек нет у всех прошлых, — отметка перед
// текущим ходом.
function markTrim(msgs, trim) {
  msgs.querySelectorAll('.trimline').forEach(x => x.remove());
  const turns = [...msgs.querySelectorAll('.turn')];
  turns.forEach(t => t.classList.remove('unseen'));
  if (!trim || (!trim.turns && !trim.excerpts)) return;
  for (let i = 1; i <= trim.turns && i < turns.length; i++) turns[i].classList.add('unseen');
  const at = turns[trim.turns ? Math.min(trim.turns + 1, turns.length - 1) : turns.length - 1];
  at?.before(el(`<div class="trimline">${trim.turns ? TRIM_TURNS : TRIM_EXCERPTS}</div>`));
}

export async function showDialog(center, id, app) {
  center.innerHTML = `<div class="dialog">
    <div class="dialog-head"><span id="conv-title"></span><span class="head-end"><span class="langsw" id="chat-lang" role="group" aria-label="Язык корпуса">
      <button type="button" data-lang="en" title="Английский корпус">EN</button><button type="button" data-lang="ru" title="Русский корпус (переводы)">RU</button>
    </span><a id="export" href="/api/conversations/${id}/export.md">ЭКСПОРТ .MD</a></span></div>
    <div class="msgs" id="msgs"></div>
    <div class="status" id="status" hidden></div>
    <div class="banner" id="banner" hidden></div>
    <!-- Подсказка без рамки: ключа нет, и ход всё равно будет отказом (см. syncKeyHint) -->
    <p class="hint" id="nokey" hidden>Ключ API не задан — задайте его в <a href="#/settings">«Настройках»</a></p>
    <form class="input" id="ask">
      <textarea id="q" rows="1" placeholder="Спросить Аристотеля…"></textarea>
      <button class="btn" type="submit" id="send">ΕΡΩΤΑΝ</button>
      <button class="btn ghost" type="button" id="stop" hidden>СТОП</button>
      <button class="btn ghost" type="button" id="again" title="Задать последний вопрос заново">ЕЩЁ РАЗ</button>
    </form>
  </div>`;
  const v = {
    id, app, msgs: center.querySelector('#msgs'), status: center.querySelector('#status'),
    banner: center.querySelector('#banner'), q: center.querySelector('#q'),
    send: center.querySelector('#send'), stop: center.querySelector('#stop'),
    again: center.querySelector('#again'), title: center.querySelector('#conv-title'),
    nokey: center.querySelector('#nokey'), controller: null,
    langBox: center.querySelector('#chat-lang'),
  };
  // Язык корпуса: EN — английский, RU — локальный русский перевод. Выбор
  // общий с библиотекой; меняет поиск следующего хода, не прошлые ответы.
  const paintLang = () => {
    const lang = getLang();
    for (const b of v.langBox.querySelectorAll('button')) {
      const on = b.dataset.lang === lang;
      b.classList.toggle('on', on);
      b.setAttribute('aria-pressed', String(on));
    }
  };
  paintLang();
  v.langBox.addEventListener('click', e => {
    const b = e.target.closest('button[data-lang]');
    if (!b || b.dataset.lang === getLang()) return;
    setLang(b.dataset.lang);
    paintLang();
    addBanner(v, b.dataset.lang === 'ru'
      ? 'следующие поиски пойдут по русскому переводу (corpus-ru)'
      : 'следующие поиски пойдут по английскому корпусу (corpus)');
  });

  let conv;
  try {
    conv = await get(`/api/conversations/${id}`);
  } catch (e) {
    center.innerHTML = `<div class="dialog"><p class="status">беседа не открылась: ${esc(e.message)}</p></div>`;
    return;
  }
  conv.items ??= [];
  v.title.textContent = conv.items.length ? (conv.title ?? '').toUpperCase() : 'НОВАЯ БЕСЕДА';
  app.onContext(conv.context_used, conv.tokens);
  syncKeyHint(v);
  // Слушатели — до отрисовки: если она упадёт на неожиданной записи, форма всё
  // равно должна работать, а не уходить нативным GET с перезагрузкой страницы.
  bindInput(v, center);
  try {
    render(v, conv);
  } catch (e) {
    console.error(e);
    v.msgs.append(el('<p class="stopped">беседа показана не полностью: ' + esc(e.message) + '</p>'));
  }
  syncAgain(v);
}

function bindInput(v, center) {
  center.querySelector('#ask').addEventListener('submit', e => {
    e.preventDefault();
    const text = v.q.value.trim();
    if (text && !v.controller) { v.q.value = ''; ask(v, text, false); }
  });
  v.q.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      center.querySelector('#ask').requestSubmit();
    }
  });
  v.stop.addEventListener('click', () => v.controller?.abort());
  v.again.addEventListener('click', () => {
    const last = [...v.msgs.querySelectorAll('.turn')].pop();
    if (!last || v.controller) return;
    const prev = last.querySelector('.ans');
    prev?.remove();
    ask(v, null, true, prev);
  });
}

function render(v, conv) {
  v.msgs.innerHTML = '';
  if (!conv.items.length) {
    const w = el(`<div class="welcome"><div class="bust"></div>
      <p class="lead">Цитаты сверяются с корпусом его сочинений; щелчок по цитате открывает главу.
      <span class="tag">ВЫВОД</span> — следствие, а не цитата; <span class="tag">ФАКТ</span> — позднейшее знание
      установило иное; <span class="tag">ПОСЫЛКА</span> — на чём держится довод.</p>
      <ul class="examples">${EXAMPLES.map(x => `<li><button type="button">${esc(x)}</button></li>`).join('')}</ul></div>`);
    w.querySelectorAll('.examples button').forEach(b => b.addEventListener('click', () => ask(v, b.textContent, false)));
    v.msgs.append(w);
    syncAgain(v);
    return;
  }
  let turn = null;
  let trim = null;
  for (const it of conv.items) {
    if (it.type === 'user') { turn = turnEl(it.text); v.msgs.append(turn); continue; }
    if (it.type === 'trim') { trim = it; continue; }
    const ans = el('<div class="ans"></div>');
    for (const b of it.blocks ?? []) { const e = blockEl(b, it.quotes_unknown); if (e) ans.append(e); }
    if (it.stop_reason === 'cancelled') ans.append(el('<p class="stopped">ответ остановлен</p>'));
    if (it.stop_reason === 'error') ans.append(el('<p class="stopped">ответ прерван ошибкой</p>'));
    if (it.usage) ans.append(metaLine(it.usage));
    (turn ?? v.msgs).append(ans);
  }
  markTrim(v.msgs, trim);
  v.msgs.scrollTop = v.msgs.scrollHeight;
  syncAgain(v);
}

function busy(v, on) {
  v.send.disabled = on; v.again.disabled = on; v.stop.hidden = !on;
}

// «Ещё раз» нужен, только когда есть последний вопрос.
function syncAgain(v) { v.again.hidden = !v.msgs.querySelector('.turn'); }

// syncKeyHint — подсказка над полем ввода, пока ключа API нет: любой вопрос был
// бы отказом сервера. Показывается, только когда сервер сказал это прямо
// (key.set === false) и без предупреждения об испорченном keys.json: там дело не
// в отсутствии ключа, а подсказка про «Настройки» увела бы не туда — ответит
// сам сервер. Не загрузилось состояние — подсказки нет: ничего не известно.
function syncKeyHint(v) {
  const key = v.app.state?.key;
  v.nokey.hidden = !(key?.set === false && !(key.warning ?? ''));
}

// addBanner — предупреждения и ошибки накапливаются: поздняя не должна
// затирать раннюю (предупреждение поиска, потом о бюджете).
function addBanner(v, text) {
  v.banner.append(el(`<div>${esc(text)}</div>`));
  v.banner.hidden = false;
}

function clearBanner(v) {
  v.banner.replaceChildren();
  v.banner.hidden = true;
}

// ask — ход: вопрос, поток ответа, итог. again — повтор последнего вопроса
// (сервер сам отзовёт прошлый ответ); prev — снятый с экрана прошлый ответ,
// его вернём, если сервер повтор не принял.
async function ask(v, text, again, prev = null) {
  if (v.controller) return;
  v.msgs.querySelector('.welcome')?.remove();
  clearBanner(v);
  let turn = [...v.msgs.querySelectorAll('.turn')].pop();
  if (!again) { turn = turnEl(text); v.msgs.append(turn); }
  const ans = el('<div class="ans"></div>');
  const live = el('<p class="live"></p>');
  ans.append(live);
  turn.append(ans);
  let liveText = '';
  let lastProse = '';
  let ended = ''; // done или error — поток закончился как положено
  let rejected = false; // сервер ход не принял: ничего не записано
  v.controller = new AbortController();
  busy(v, true);
  syncAgain(v);

  // Строка состояния: что идёт сейчас и сколько уже длится ход. Размышление
  // модели занимает до минуты на запрос, и без счётчика ожидание неотличимо от
  // зависания. Первое слово ответа строку прячет.
  const started = Date.now();
  let phase = '';
  const showStatus = () => {
    const s = Math.floor((Date.now() - started) / 1000);
    v.status.textContent = `${phase} · ${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  };
  const setPhase = p => { phase = p; v.status.hidden = false; showStatus(); };
  const clock = setInterval(() => { if (!v.status.hidden) showStatus(); }, 1000);

  // Вопрос только что отправлен — смотрим в конец. Дальше за ответом идём, пока
  // читатель сам не отлистал вверх. Флаг меняют только события прокрутки.
  // Уменьшение scrollTop — ещё не уход читателя вверх: его даёт и браузер, когда
  // содержимое сжимается (пустеющая живая строка, убранное приветствие) или
  // растёт сама область (скрытая строка поиска), и наш собственный сдвиг к новому,
  // меньшему концу. Тогда читатель остаётся у конца (отставание ~0), и следование
  // нужно сохранить; вверх он ушёл, только если от конца его отделяет заметное
  // расстояние. Вернулся к концу (с запасом на строку, успевшую прийти до самого
  // события) — включаем. Замер «далеко ли до конца» перед каждым событием потока
  // терялся на высоте нового хода и так отключал следование насовсем.
  const bottomGap = () => v.msgs.scrollHeight - v.msgs.scrollTop - v.msgs.clientHeight;
  let follow = true;
  let lastTop = v.msgs.scrollTop;
  const onScroll = () => {
    const top = v.msgs.scrollTop;
    if (top < lastTop - 1 && bottomGap() > 2) follow = false;
    else if (bottomGap() < 80) follow = true;
    lastTop = top;
  };
  v.msgs.addEventListener('scroll', onScroll);
  const toBottom = () => { if (follow) v.msgs.scrollTop = v.msgs.scrollHeight; };
  toBottom();

  // Недописанная строка, которую блоком ещё не стало: ход оборвался, а то,
  // что успело прийти, остаётся. Строка-маркер (>>) без конца не показывается.
  const keepLive = () => {
    if (liveText.trim() && !liveText.startsWith('>>') && liveText !== lastProse) {
      live.className = '';
      live.textContent = liveText;
    }
  };
  const notice = msg => ans.append(el(`<p class="stopped">${esc(msg)}</p>`));

  try {
    const url = `/api/conversations/${v.id}/${again ? 'again' : 'turns'}` + langParam();
    let res;
    try {
      res = await fetch(url, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(again ? {} : { text }),
        signal: v.controller.signal,
      });
      if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
    } catch (e) {
      if (e.name !== 'AbortError') rejected = true;
      throw e;
    }
    await readSSE(res, (event, d) => {
      switch (event) {
        case 'delta': {
          v.status.hidden = true;
          // Законченные строки придут блоками; здесь — только недописанная.
          liveText += d.text ?? '';
          const nl = liveText.lastIndexOf('\n');
          if (nl >= 0) liveText = liveText.slice(nl + 1);
          const marker = liveText.startsWith('>>');
          live.className = marker ? 'live marker' : 'live';
          live.textContent = marker ? '…' : liveText;
          break;
        }
        case 'block': {
          if (d.kind === 'prose') lastProse = d.text ?? ''; // у пустой строки ключа text нет
          const e = blockEl(d);
          if (e) ans.insertBefore(e, live);
          break;
        }
        case 'thinking': setPhase('размышляю'); break;
        case 'search': setPhase(`ищу в корпусе: «${d.query ?? ''}»`); break;
        case 'warning': addBanner(v, d.text ?? ''); break;
        case 'trim': markTrim(v.msgs, d); break;
        // Расход приходит и без done: при ошибке он идёт перед error.
        case 'usage':
          ans.append(metaLine(d.usage));
          // Шапка принадлежит открытой беседе: ход мог идти, пока человек ушёл в другую.
          if (v.app.currentId === v.id) v.app.onUsage(d);
          break;
        case 'error':
          ended = 'error';
          addBanner(v, 'ошибка: ' + (d.message ?? 'без описания'));
          notice('ответ прерван ошибкой');
          break;
        case 'done':
          ended = 'done';
          if (d.title) v.title.textContent = d.title.toUpperCase();
          v.app.onDone(v.id, d.title);
          break;
      }
      toBottom();
    });
    // Поток кончился без done и без error: сервер остановили на ходу. То, что
    // пришло, остаётся; ход журналом записан как остановленный.
    if (!ended) { keepLive(); notice('ответ оборван: соединение закрыто'); toBottom(); }
  } catch (e) {
    if (rejected) {
      // Сервер ход не принял (нет ключа, занято…): вопрос возвращается в поле,
      // а на экране остаётся то, что было до попытки.
      ans.remove();
      if (again) { if (prev) turn.append(prev); } else {
        turn.remove();
        if (!v.q.value) v.q.value = text;
        if (!v.msgs.querySelector('.turn')) render(v, { items: [] });
      }
      addBanner(v, e.message);
    } else if (e.name === 'AbortError') {
      keepLive();
      notice('ответ остановлен');
      toBottom();
    } else {
      // Сбой уже на нашей стороне (разбор события, обработчик) или обрыв сети.
      // Соединение закрываем сами, иначе сервер дописывает ход и держит беседу
      // занятой: следующий вопрос получил бы 409.
      console.error(e);
      v.controller.abort();
      keepLive();
      notice('ответ оборван: ' + e.message);
      toBottom();
    }
  } finally {
    v.msgs.removeEventListener('scroll', onScroll);
    if (live.classList.contains('live')) live.remove();
    clearInterval(clock);
    v.status.hidden = true;
    v.controller = null;
    busy(v, false);
    syncAgain(v);
    if (ended !== 'done') v.app.refreshConvs();
    // Итог месяца вырос, а ключ могли задать или убрать в другом окне или из
    // командной строки: состояние перечитывается после любого хода, подсказка
    // о ключе — по нему. Не вышло — остаётся прежнее, шапка подождёт.
    v.app.refreshState().then(() => syncKeyHint(v), () => {});
    v.q.focus();
  }
}
