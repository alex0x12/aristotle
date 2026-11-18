import { get, esc, usageText, usageIsPartial } from './api.js';
import { readSSE } from './stream.js';
import { openSource } from './sources.js';

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
  if (b.kind === 'prose') return b.text.trim() ? el(`<p>${esc(b.text)}</p>`) : null;
  if (b.kind === 'quote') {
    const mark = quotesUnknown ? '<span class="unk">сверка не сохранена</span>'
      : b.verified ? '' : '<span class="warn">нет в выдержках</span>';
    const q = el(`<blockquote class="quote${b.verified || quotesUnknown ? '' : ' bad'}" title="Открыть главу">
      ${esc(b.ru || b.en)}<cite>${esc(b.cite || 'без ссылки')}${mark}</cite></blockquote>`);
    q.addEventListener('click', () => openSource(quotesUnknown ? { ...b, unknown: true } : b));
    return q;
  }
  return el(`<p class="note"><span class="tag">${NOTE[b.kind] ?? esc(b.kind)}</span> ${esc(b.text)}</p>`);
}

function metaLine(usage, cost) {
  const line = el(`<div class="meta-line">${esc(usageText(usage, cost))}</div>`);
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
    <div class="dialog-head"><span id="conv-title"></span><a id="export" href="/api/conversations/${id}/export.md">ЭКСПОРТ .MD</a></div>
    <div class="msgs" id="msgs"></div>
    <div class="status" id="status" hidden></div>
    <div class="banner" id="banner" hidden></div>
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
    controller: null,
  };
  let conv;
  try {
    conv = await get(`/api/conversations/${id}`);
  } catch (e) {
    center.innerHTML = `<div class="dialog"><p class="status">беседа не открылась: ${esc(e.message)}</p></div>`;
    return;
  }
  v.title.textContent = conv.items.length ? conv.title.toUpperCase() : 'НОВАЯ БЕСЕДА';
  render(v, conv);
  app.onContext(conv.context_used, conv.tokens, conv.cost_usd);

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
    if (it.usage) ans.append(metaLine(it.usage, it.cost_usd));
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
    const url = `/api/conversations/${v.id}/${again ? 'again' : 'turns'}`;
    let res;
    try {
      res = await fetch(url, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(again ? { profile: v.app.profile } : { text, profile: v.app.profile }),
        signal: v.controller.signal,
      });
      if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
    } catch (e) {
      if (e.name !== 'AbortError') rejected = true;
      throw e;
    }
    await readSSE(res, (event, d) => {
      const stick = v.msgs.scrollHeight - v.msgs.scrollTop - v.msgs.clientHeight < 60;
      switch (event) {
        case 'delta': {
          v.status.hidden = true;
          // Законченные строки придут блоками; здесь — только недописанная.
          liveText += d.text;
          const nl = liveText.lastIndexOf('\n');
          if (nl >= 0) liveText = liveText.slice(nl + 1);
          const marker = liveText.startsWith('>>');
          live.className = marker ? 'live marker' : 'live';
          live.textContent = marker ? '…' : liveText;
          break;
        }
        case 'block': {
          if (d.kind === 'prose') lastProse = d.text;
          const e = blockEl(d);
          if (e) ans.insertBefore(e, live);
          break;
        }
        case 'search': v.status.hidden = false; v.status.textContent = `ищу в корпусе: «${d.query}»…`; break;
        case 'warning': addBanner(v, d.text); break;
        case 'trim': markTrim(v.msgs, d); break;
        // Расход приходит и без done: при ошибке он идёт перед error.
        case 'usage':
          ans.append(metaLine(d.usage, d.cost_usd));
          // Шапка принадлежит открытой беседе: ход мог идти, пока человек ушёл в другую.
          if (v.app.currentId === v.id) v.app.onUsage(d);
          break;
        case 'error':
          ended = 'error';
          addBanner(v, 'ошибка: ' + d.message);
          notice('ответ прерван ошибкой');
          break;
        case 'done':
          ended = 'done';
          if (d.title) v.title.textContent = d.title.toUpperCase();
          v.app.onDone(v.id, d.title);
          break;
      }
      if (stick) v.msgs.scrollTop = v.msgs.scrollHeight;
    });
    // Поток кончился без done и без error: сервер остановили на ходу. То, что
    // пришло, остаётся; ход журналом записан как остановленный.
    if (!ended) { keepLive(); notice('ответ оборван: соединение закрыто'); }
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
    } else {
      keepLive();
      notice('ответ оборван: ' + e.message);
    }
  } finally {
    if (live.classList.contains('live')) live.remove();
    v.status.hidden = true;
    v.controller = null;
    busy(v, false);
    syncAgain(v);
    if (ended !== 'done') v.app.refreshConvs();
    v.q.focus();
  }
}
