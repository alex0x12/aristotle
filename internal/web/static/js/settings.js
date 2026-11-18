import { get, send, esc, fmtTokens, fmtCost } from './api.js';

const EFFORT = ['', 'low', 'medium', 'high'];

// draft — настройки, как их правит экран; уходят на сервер целиком по «СОХРАНИТЬ».
// draft.removed — имена профилей, которые пользователь убрал сам: только по этому
// списку сервер удаляет их ключи из keys.json. Пропавший из документа профиль без
// имени в списке (устаревшая вкладка) ключа не лишается.
// fresh — профили, которых на сервере ещё нет: имя у них правится, а ключ
// сохранить нельзя (сервер знает только профили из конфига).
let draft = null;
let fresh = new WeakSet();
// notes — итог последнего действия над ключом профиля («ключ принят»); живёт
// отдельно от разметки, потому что карточки перерисовываются после каждого.
let notes = {};
let dirty = false;

// Рассуждающая модель — только у OpenAI-совместимого профиля: Anthropic-провайдер
// reasoning не читает. У неё нет температуры, а усилие есть; у обычной — наоборот.
const isReasoning = p => p.kind === 'openai' && !!p.reasoning;

// settle приводит профиль к тому, что принимает сервер: лишнее поле у него —
// отказ, а не тихое «ладно». Так же чистится профиль, пришедший из файла,
// правленного вручную: оба поля там и так не действовали.
function settle(p) {
  if (p.kind !== 'openai') p.reasoning = false;
  if (isReasoning(p)) p.temperature = null; else p.reasoning_effort = '';
  return p;
}

function load(doc) {
  draft = doc;
  draft.removed = [];
  draft.profiles = (draft.profiles ?? []).map(settle);
  fresh = new WeakSet();
  dirty = false;
}

const reason = e => (e instanceof TypeError ? 'нет связи с сервером' : e.message);

// ---- профили и ключи ----

function keyRow(p) {
  const name = esc(p.name ?? '');
  const key = p.key ?? { source: 'none' };
  // ok: true — удача, false — отказ (в рамке, как пометка «нет в выдержках»), null — ещё идёт.
  const n = notes[p.name];
  const note = n ? `<span class="${n.ok === false ? 'warn' : 'hint'}">${esc(n.text)}</span>` : '';
  const field = (off, hintText) => `<span class="hint">${hintText}</span>
    <input type="password" autocomplete="new-password" placeholder="новый ключ" data-key="${name}"${off ? ' disabled' : ''}>`;
  const btn = (act, label, off) => `<button class="btn ghost" data-act="${act}" data-name="${name}" type="button"${off ? ' disabled' : ''}>${label}</button>`;
  if (fresh.has(p)) {
    return `<span class="hint">ключ можно ввести после сохранения профиля</span>`;
  }
  if (key.warning) {
    return `${field(true, esc(key.warning) + ': ключи пока не изменить')}${btn('save-key', 'ЗАМЕНИТЬ', true)}${btn('check', 'ПРОВЕРИТЬ', true)}`;
  }
  if (key.source === 'env') {
    return `${field(true, `задан переменной ${esc(p.key_env ?? '')} (${esc(key.hint ?? '')}) — меняется в окружении`)}
      ${btn('save-key', 'ЗАМЕНИТЬ', true)}${btn('del-key', 'УДАЛИТЬ', true)}${btn('check', 'ПРОВЕРИТЬ', false)}${note}`;
  }
  if (key.source === 'file') {
    return `${field(false, `сохранён (${esc(key.hint ?? '')})`)}${btn('save-key', 'ЗАМЕНИТЬ', false)}${btn('del-key', 'УДАЛИТЬ', false)}${btn('check', 'ПРОВЕРИТЬ', false)}${note}`;
  }
  return `${field(false, 'ключа нет')}${btn('save-key', 'СОХРАНИТЬ', false)}${note}`;
}

function profileCard(p, i) {
  const v = x => esc(x ?? '');
  const num = (k, val, ph, title = '') =>
    `<input type="number" step="any" min="0" data-i="${i}" data-k="${k}" value="${v(val)}" placeholder="${v(ph)}"${title ? ` title="${v(title)}" aria-label="${v(title)}"` : ''}>`;
  const pr = p.prices ?? {};
  const openai = p.kind === 'openai';
  const name = fresh.has(p)
    ? `<input data-i="${i}" data-k="name" value="${v(p.name)}" placeholder="имя-профиля" maxlength="32" autocomplete="off">`
    : `${v(p.name)}${p.builtin ? '<span class="badge">встроенный</span>' : ''}`;
  return `<div class="card"><h3>${name}</h3>
    <div class="grid">
      <label>Тип</label><select data-i="${i}" data-k="kind">
        <option value="openai"${openai ? ' selected' : ''}>OpenAI-совместимый</option>
        <option value="anthropic"${openai ? '' : ' selected'}>Anthropic</option></select>
      ${openai ? `<label>Адрес API</label><input data-i="${i}" data-k="base_url" value="${v(p.base_url)}" placeholder="https://…/v1" autocomplete="off">` : ''}
      <label>Модель</label><input data-i="${i}" data-k="model" value="${v(p.model)}" autocomplete="off">
      <label>Переменная ключа</label><input data-i="${i}" data-k="key_env" value="${v(p.key_env)}" placeholder="необязательно" autocomplete="off">
      ${openai ? `<label>Рассуждающая модель</label><input type="checkbox" data-i="${i}" data-k="reasoning"${p.reasoning ? ' checked' : ''}>` : ''}
      ${isReasoning(p) ? `<label>Усилие рассуждения</label><select data-i="${i}" data-k="reasoning_effort">${
        EFFORT.map(e => `<option value="${e}"${(p.reasoning_effort ?? '') === e ? ' selected' : ''}>${e || 'умолчание'}</option>`).join('')}</select>` : ''}
      ${isReasoning(p) ? '' : `<label>Температура</label>${num('temperature', p.temperature, 'умолчание')}`}
      <label>Потолок ответа, токенов</label>${num('max_output_tokens', p.max_output_tokens || '', 'умолчание')}
      <label>Цены, $ за 1M токенов</label><span class="prices">${num('prices.input', pr.input, 'вход', 'вход')}${num('prices.output', pr.output, 'вывод', 'вывод')}${num('prices.cache_read', pr.cache_read, 'кэш', 'чтение из кэша')}${num('prices.cache_write', pr.cache_write, 'в кэш', 'запись в кэш')}</span>
      <label>Ключ</label><span class="keyrow">${keyRow(p)}</span>
    </div>
    ${p.builtin ? '' : `<div class="row"><button class="btn ghost" data-act="drop" data-i="${i}" type="button">УБРАТЬ ПРОФИЛЬ</button></div>`}
  </div>`;
}

// setField переносит значение поля в черновик; true — перерисовать вкладку
// (после него меняется набор полей).
function setField(p, k, t) {
  if (k === 'reasoning') { p.reasoning = t.checked; settle(p); return true; }
  if (k === 'kind') {
    p.kind = t.value;
    // Адрес чужого API при смене типа не должен остаться: к нему пойдёт проверка ключа.
    if (p.kind !== 'openai') p.base_url = '';
    settle(p);
    return true;
  }
  if (k.startsWith('prices.')) {
    p.prices = p.prices ?? { input: 0, output: 0, cache_read: 0, cache_write: 0 };
    p.prices[k.slice(7)] = t.value === '' ? 0 : Number(t.value);
    if (Object.values(p.prices).every(x => !x)) p.prices = null;
    return false;
  }
  if (k === 'temperature') { p.temperature = t.value === '' ? null : Number(t.value); return false; }
  if (k === 'max_output_tokens' || k === 'context_budget') { p[k] = t.value === '' ? 0 : Number(t.value); return false; }
  p[k] = t.value;
  return false;
}

const status = (box, html) => { box.querySelector('#status').innerHTML = html; };

// redraw перерисовывает вкладку и возвращает в неё уже набранные, но не
// отправленные ключи: «СОХРАНИТЬ НАСТРОЙКИ» не должно стирать их молча.
function redraw(box, render) {
  const typed = {};
  box.querySelectorAll('input[data-key]').forEach(i => { if (i.value) typed[i.dataset.key] = i.value; });
  render();
  for (const [name, value] of Object.entries(typed)) {
    const input = [...box.querySelectorAll('input[data-key]')].find(i => i.dataset.key === name);
    if (input && !input.disabled) input.value = value;
  }
}

// save отправляет черновик; ответ сервера — уже проверенные настройки, с ним
// и перерисовывается вкладка (порядок профилей на сервере по имени).
async function save(box, app, render) {
  box.querySelector('#err').hidden = true;
  // Профиль, заведённый заново под тем же именем, больше не «убран». Сверка
  // здесь, а не на каждую набранную букву: имя набирается по знаку, и «tmp» на
  // пути к «tmpx» не должно вычеркнуть убранный «tmp».
  draft.removed = draft.removed.filter(n => !draft.profiles.some(p => p.name === n));
  try {
    load(await send('PUT', '/api/settings', draft));
  } catch (e) {
    box.querySelector('#err').hidden = false;
    box.querySelector('#err').textContent = reason(e);
    return;
  }
  redraw(box, render);
  status(box, 'сохранено');
  try {
    await app.refreshState(); // выбор профиля в шапке и бюджет контекста — по новым настройкам
    app.onContext(app.lastUsed);
  } catch { /* настройки уже сохранены; шапка обновится при следующем обращении */ }
}

const saveBar = () => `<div class="banner" id="err" hidden></div>
  <div class="row"><button class="btn" id="save" type="button">СОХРАНИТЬ НАСТРОЙКИ</button><span class="hint" id="status"></span></div>`;

function touched(box) {
  if (!dirty) { dirty = true; status(box, 'есть несохранённые изменения'); }
}

function profilesTab(box, app) {
  const render = () => profilesTab(box, app);
  const broken = draft.profiles.some(p => p.key?.warning);
  box.innerHTML =
    (broken ? `<div class="banner">Файл ключей keys.json не читается: ключи пока нельзя ни показать, ни изменить. Исправьте или удалите его вручную и откройте экран заново; остальные настройки сохраняются как обычно.</div>` : '') +
    draft.profiles.map(profileCard).join('') +
    `<div class="row"><button class="btn ghost" id="add" type="button">ДОБАВИТЬ ПРОФИЛЬ</button></div>` + saveBar();
  if (dirty) status(box, 'есть несохранённые изменения');
  box.oninput = box.onchange = e => {
    const t = e.target;
    if (!t.dataset.k) return;
    touched(box);
    if (setField(draft.profiles[+t.dataset.i], t.dataset.k, t)) redraw(box, render);
  };
  box.querySelector('#add').onclick = () => {
    const p = settle({ name: '', builtin: false, kind: 'openai', base_url: 'https://', model: '', key_env: '',
      reasoning: false, reasoning_effort: '', temperature: null, max_output_tokens: 0, context_budget: 64000, prices: null,
      key: { source: 'none' } });
    fresh.add(p);
    draft.profiles.push(p);
    touched(box);
    redraw(box, render);
    box.querySelectorAll('input[data-k="name"]').forEach(i => { if (!i.value) i.focus(); });
  };
  box.querySelector('#save').onclick = () => save(box, app, render);
  // Enter в поле ключа — «СОХРАНИТЬ/ЗАМЕНИТЬ» этого профиля.
  box.onkeydown = e => {
    if (e.key !== 'Enter' || !e.target.dataset.key || e.isComposing) return;
    e.preventDefault();
    [...box.querySelectorAll('button[data-act="save-key"]')].find(b => b.dataset.name === e.target.dataset.key)?.click();
  };
  box.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b || b.disabled) return;
    const name = b.dataset.name;
    if (b.dataset.act === 'drop') {
      const p = draft.profiles[+b.dataset.i];
      const withKey = !fresh.has(p) && p.key?.source === 'file';
      if (withKey && !confirm(`Убрать профиль ${p.name}? Его сохранённый ключ тоже будет удалён при сохранении настроек.`)) return;
      // Решение убрать принято (подтверждение, если оно нужно, получено): ключ
      // этого профиля удалится при сохранении. Заведённый на экране и не
      // сохранённый профиль на сервере не существует — в список он не идёт.
      if (!fresh.has(p) && !draft.removed.includes(p.name)) draft.removed.push(p.name);
      draft.profiles.splice(+b.dataset.i, 1);
      touched(box);
      redraw(box, render);
      return;
    }
    b.disabled = true;
    try {
      if (b.dataset.act === 'save-key') {
        const input = [...box.querySelectorAll('input[data-key]')].find(i => i.dataset.key === name);
        await send('PUT', '/api/keys/' + encodeURIComponent(name), { key: input?.value ?? '' });
        if (input) input.value = ''; // иначе redraw вернул бы набранный ключ в поле
        notes[name] = { ok: true, text: 'ключ сохранён' };
      } else if (b.dataset.act === 'del-key') {
        if (!confirm(`Удалить сохранённый ключ профиля ${name}?`)) { b.disabled = false; return; }
        await send('DELETE', '/api/keys/' + encodeURIComponent(name));
        notes[name] = { ok: true, text: 'ключ удалён' };
      } else if (b.dataset.act === 'check') {
        notes[name] = { ok: null, text: 'проверяю…' };
        redraw(box, render);
        const r = await send('POST', `/api/keys/${encodeURIComponent(name)}/check`);
        notes[name] = { ok: !!r.ok, text: r.message ?? '' };
      }
    } catch (err) {
      notes[name] = { ok: false, text: reason(err) };
    }
    // Ключи правятся отдельно от черновика: перечитывается только то, что про
    // ключи, а несохранённые правки полей остаются.
    try {
      const server = await get('/api/settings');
      for (const p of draft.profiles) {
        const sp = server.profiles?.find(x => x.name === p.name);
        if (sp) p.key = sp.key;
      }
    } catch { /* состояние ключа останется прежним, сообщение об ошибке уже показано */ }
    redraw(box, render);
  };
}

// ---- контекст и корпус ----

function contextTab(box, app) {
  const render = () => contextTab(box, app);
  box.innerHTML = `<div class="card"><h3>КОРПУС</h3><div class="grid">
      <label>Глав на один поиск</label><input type="number" min="1" max="20" id="k" value="${esc(draft.corpus_k ?? 6)}">
      <label>Профиль по умолчанию</label><select id="def">${draft.profiles.map(p =>
        `<option${p.name === draft.default ? ' selected' : ''}>${esc(p.name)}</option>`).join('')}</select></div></div>
    <div class="card"><h3>БЮДЖЕТ КОНТЕКСТА</h3><p class="hint">Потолок входа запроса в токенах; 0 — без урезания.
      Когда беседа его превышает, сперва опускаются старые выдержки, потом старые ходы; первый вопрос остаётся.</p>
      <div class="grid">${draft.profiles.map((p, i) =>
        `<label>${esc(p.name)}</label><input type="number" min="0" step="1000" data-i="${i}" data-k="context_budget" value="${esc(p.context_budget ?? 0)}">`).join('')}</div></div>` + saveBar();
  if (dirty) status(box, 'есть несохранённые изменения');
  box.oninput = box.onchange = e => {
    const t = e.target;
    if (t.id === 'k') draft.corpus_k = Number(t.value);
    else if (t.id === 'def') draft.default = t.value;
    else if (t.dataset.k) setField(draft.profiles[+t.dataset.i], t.dataset.k, t);
    else return;
    touched(box);
  };
  box.onclick = null;
  box.querySelector('#save').onclick = () => save(box, app, render);
}

// ---- расход ----

const ruDate = s => String(s ?? '').split('-').reverse().join('.');

// Числа строки неполны, если среди её ответов были оборванные или без чисел
// от провайдера (partial): тогда «≈», а когда считать нечего — прочерк.
function tokenCell(r, n) {
  const any = ['input', 'output', 'cache_read', 'cache_write'].some(k => r[k]);
  if (r.partial && !any) return '—';
  return (r.partial ? '≈' : '') + fmtTokens(n ?? 0);
}

// Цены нет ни у одного ответа — «—»; у части ответов нет или строка неполная —
// сумма занижена, «≈».
function costCell(r) {
  if (r.cost_usd == null) return '—';
  return (r.partial || r.cost_partial ? '≈' : '') + fmtCost(r.cost_usd);
}

async function usageTab(box) {
  box.oninput = box.onchange = box.onclick = null;
  box.innerHTML = '<p class="hint">загружаю…</p>';
  let u;
  try {
    u = await get('/api/usage');
  } catch (e) {
    box.innerHTML = `<div class="banner">расход не загрузился: ${esc(reason(e))}</div>`;
    return;
  }
  const row = (r, total) => `<tr${total ? ' class="total"' : ''}><td>${total ? 'ИТОГО' : esc(ruDate(r.day))}</td><td>${total ? '' : esc(r.profile || '—')}</td>
    <td>${esc(r.answers ?? 0)}</td><td>${esc(tokenCell(r, r.input))}</td><td>${esc(tokenCell(r, r.cache_read))}</td>
    <td>${esc(tokenCell(r, r.cache_write))}</td><td>${esc(tokenCell(r, r.output))}</td><td>${esc(costCell(r))}</td></tr>`;
  const rows = u.rows ?? [];
  box.innerHTML = `<p class="hint">${esc(ruDate(u.from))} — ${esc(ruDate(u.to))}. Знак ≈ — сумма неполная: ответ оборвался и провайдер не назвал
    точные числа, либо у ответа нет цены. Прочерк — данных нет.</p>
    <table class="usage"><tr><th>ДЕНЬ</th><th>ПРОФИЛЬ</th><th>ОТВЕТОВ</th><th>ВХОД</th><th>КЭШ</th><th>В КЭШ</th><th>ВЫВОД</th><th>$</th></tr>
    ${rows.map(r => row(r, false)).join('')}${row(u.total ?? {}, true)}</table>
    ${rows.length ? '' : '<p class="hint">За этот период ответов нет.</p>'}`;
}

export async function showSettings(center, app) {
  center.innerHTML = `<div class="settings"><div class="tabs">
      <button data-tab="profiles" type="button">ПРОФИЛИ И КЛЮЧИ</button>
      <button data-tab="context" type="button">КОНТЕКСТ И КОРПУС</button>
      <button data-tab="usage" type="button">РАСХОД</button></div><div id="tab"></div></div>`;
  const box = center.querySelector('#tab');
  notes = {};
  try {
    load(await get('/api/settings'));
  } catch (e) {
    box.innerHTML = `<div class="banner">настройки не прочитались: ${esc(reason(e))}</div>`;
    return;
  }
  const tabs = { profiles: () => profilesTab(box, app), context: () => contextTab(box, app), usage: () => usageTab(box) };
  center.querySelectorAll('.tabs button').forEach(b => b.addEventListener('click', () => {
    center.querySelectorAll('.tabs button').forEach(x => x.classList.toggle('on', x === b));
    tabs[b.dataset.tab]();
  }));
  center.querySelector('.tabs button').click();
}
