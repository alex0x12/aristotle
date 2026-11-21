import { get, send, esc, fmtTokens } from './api.js';

// Одна модель и один ключ (решение автора). Экран — две вкладки: «Настройки»
// (адрес API, модель, усилие размышления, бюджет контекста и отдельным блоком
// ключ) и «Расход».
// Ничего больше в настройках нет: ни профилей, ни цен, ни «рассуждающей модели».

const reason = e => (e instanceof TypeError ? 'нет связи с сервером' : e.message);

// pane — свой контейнер у каждого показа вкладки. Ответ запроса, пришедший, когда
// человек уже перешёл на другую вкладку, дописывается в отсоединённый контейнер и
// ничего не портит; писать в общий box после await нельзя.
function pane(box) {
  const p = document.createElement('div');
  box.replaceChildren(p);
  return p;
}

// ---- настройки и ключ ----

const FORM = `
  <div class="card"><h3>Подключение</h3>
    <div class="grid">
      <label for="f-free">Бесплатные OpenCode</label>
      <div class="field"><select id="f-free"><option value="">— выбрать модель —</option></select>
        <p class="hint">Подставляет адрес OpenCode и выбранную бесплатную модель. Для них нужен ключ
          <code>public</code>: если ключа ещё нет, он подставится в поле ниже — останется нажать «Сохранить ключ».</p></div>
      <label for="f-base">Адрес API</label>
      <div class="field"><input id="f-base" type="text" inputmode="url" autocomplete="off" spellcheck="false" placeholder="https://api.deepseek.com/v1">
        <p class="hint">Адрес API, совместимого с OpenAI, например https://api.deepseek.com/v1. Ключ отправляется на этот адрес.</p></div>
      <label for="f-model">Модель</label>
      <div class="field"><input id="f-model" type="text" autocomplete="off" spellcheck="false" placeholder="deepseek-flash">
        <p class="hint">Название модели у этого провайдера, например deepseek-flash.</p></div>
      <label for="f-effort">Усилие размышления</label>
      <div class="field"><select id="f-effort">
          <option value="low">low</option>
          <option value="high">high</option>
          <option value="max">max</option>
        </select>
        <p class="hint">Сколько модель думает перед каждым шагом ответа; по умолчанию high.
          С max она думает около минуты на каждый запрос, а за ход их бывает до пяти — ответ ждётся несколько минут.</p></div>
      <label for="f-budget">Бюджет контекста</label>
      <div class="field"><div class="stepper"><input id="f-budget" type="number" min="0" step="1" autocomplete="off" placeholder="128000">
          <button class="btn ghost" id="b-dec" type="button" aria-label="Меньше на 8000" title="Меньше на 8000">–</button>
          <button class="btn ghost" id="b-inc" type="button" aria-label="Больше на 8000" title="Больше на 8000">+</button></div>
        <p class="hint">Потолок входа запроса в токенах; 0 — без урезания, модели уходит вся беседа.
          Когда беседа превышает потолок, сперва опускаются старые выдержки, потом старые ходы; первый вопрос остаётся.</p></div>
    </div>
    <div class="banner" id="err" hidden></div>
    <div class="row"><button class="btn" id="save" type="button">Сохранить</button><span class="hint" id="status"></span></div>
    <p class="hint" id="backup" hidden></p>
  </div>
  <div class="card"><h3>Ключ API</h3>
    <p class="hint" id="key-status"></p>
    <div class="keyrow">
      <input type="password" id="key-input" autocomplete="new-password" spellcheck="false" placeholder="новый ключ" aria-label="Ключ API">
      <button class="btn ghost" id="key-save" type="button">Сохранить ключ</button>
      <button class="btn ghost" id="key-del" type="button">Удалить ключ</button>
      <button class="btn ghost" id="key-check" type="button">Проверить</button>
    </div>
    <p class="keynote" id="key-note" hidden></p>
    <p class="hint">Ключ хранится в файле keys.json рядом с настройками, в браузер приходят только его последние четыре знака.
      «Проверить» проверяет сохранённый ключ по сохранённому адресу API: это запрос списка моделей.
      Если keys.json не читается, исправьте или удалите файл вручную: настройки выше от этого не зависят.</p>
  </div>`;

// paintKey — строка состояния ключа. key.hint и key.warning приходят не всегда
// (omitempty): без них это «не задан» либо «задан» без концовки. Испорченный
// keys.json — предупреждение сервера в рамке вместо обычной строки.
function paintKey(el, key) {
  const warning = key?.warning ?? '';
  const hint = key?.hint ?? '';
  el.className = warning ? 'banner' : 'hint';
  el.textContent = warning || ((key?.set ?? false) ? 'ключ задан' + (hint ? ` (${hint})` : '') : 'ключ не задан');
}

async function settingsTab(box, app) {
  const p = pane(box);
  p.innerHTML = '<p class="hint">загружаю…</p>';
  let doc;
  try {
    doc = await get('/api/settings');
  } catch (e) {
    p.innerHTML = `<div class="banner">настройки не прочитались: ${esc(reason(e))}</div>`;
    return;
  }
  p.innerHTML = FORM;
  const $ = s => p.querySelector(s);
  const f = { free: $('#f-free'), base: $('#f-base'), model: $('#f-model'), effort: $('#f-effort'), budget: $('#f-budget') };
  let freeBase = ''; // адрес из /api/opencode/models; пустой — список ещё не загружен
  const els = {
    err: $('#err'), status: $('#status'), backup: $('#backup'),
    keyStatus: $('#key-status'), key: $('#key-input'), note: $('#key-note'),
  };
  const buttons = [...p.querySelectorAll('button')];
  // Значения кладутся свойством value, а не разметкой: адрес и модель — чужой
  // ввод, а ключа в разметке не бывает вовсе.
  // syncFree — выбрать в списке модель, если она уже стоит в поле «Модель»;
  // иначе оставить «— выбрать модель —».
  const syncFree = () => {
    const model = f.model.value.trim();
    const known = [...f.free.options].some(o => o.value === model && o.value !== '');
    f.free.value = known ? model : '';
  };
  const fill = d => {
    f.base.value = d?.base_url ?? '';
    f.model.value = d?.model ?? '';
    f.effort.value = d?.reasoning_effort ?? 'high';
    f.budget.value = String(d?.context_budget ?? 0);
    syncFree();
  };
  fill(doc);
  paintKey(els.keyStatus, doc?.key);

  // Список бесплатных моделей — с сервера: он спрашивает opencode CLI, затем
  // его кеш, затем отдаёт встроенный список. Не удалось — список пуст, поля
  // адреса и модели остаются доступны вручную.
  (async () => {
    try {
      const fm = await get('/api/opencode/models');
      freeBase = fm?.base_url ?? '';
      for (const m of fm?.models ?? []) {
        const o = document.createElement('option');
        o.value = m.id;
        o.textContent = m.name || m.id;
        f.free.append(o);
      }
      f.free.disabled = false;
      syncFree();
    } catch {
      f.free.disabled = true;
    }
  })();

  f.free.addEventListener('change', () => {
    if (!f.free.value || busy) return;
    f.base.value = freeBase || f.base.value;
    f.model.value = f.free.value;
    els.status.textContent = 'есть несохранённые изменения';
    // У бесплатных моделей ключ — public. Заданный ключ не трогаем: у него
    // может быть свой консольный доступ.
    if (!(doc?.key?.set ?? false) && !els.key.value) {
      els.key.value = 'public';
      els.note.hidden = false;
      els.note.textContent = 'для бесплатных моделей ключ public — нажмите «Сохранить ключ»';
    } else if (doc?.key?.set ?? false) {
      els.note.hidden = false;
      els.note.textContent = 'ключ уже задан; для бесплатных моделей нужен public — проверьте блок «Ключ API»';
    }
  });

  // Запрос в пути — все кнопки выключены, поля только для чтения: двойной щелчок
  // или Enter не отправят то же второй раз, а ответ не затрёт набираемое. Поля не
  // выключаются совсем: у выключенного поля фокус пропал бы.
  let busy = false;
  const lock = on => {
    busy = on;
    buttons.forEach(b => { b.disabled = on; });
    [f.base, f.model, f.budget, els.key].forEach(i => { i.readOnly = on; });
  };
  const run = async fn => {
    if (busy) return;
    const had = document.activeElement;
    lock(true);
    try { await fn(); } finally { lock(false); }
    if (had?.isConnected && p.contains(had)) had.focus(); // выключенная кнопка теряет фокус
  };
  const refresh = async () => {
    try { await app.refreshState(); } catch { /* уже сохранено; шапка обновится при следующем обращении */ }
  };

  f.effort.addEventListener('change', () => { if (!busy) els.status.textContent = 'есть несохранённые изменения'; });
  for (const i of [f.base, f.model, f.budget]) {
    i.addEventListener('input', () => { if (!busy) els.status.textContent = 'есть несохранённые изменения'; });
    i.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); $('#save').click(); }
    });
  }
  // Шаг бюджета — свои кнопки и ↑/↓ в поле: родная стрелка браузера серая и
  // выбивается из оформления (её прячет CSS). Число по-прежнему можно набрать.
  const STEP = 8000;
  const bump = dir => {
    if (busy) return;
    const raw = f.budget.value.trim();
    const n = Number(raw);
    const cur = raw !== '' && Number.isFinite(n) ? Math.round(n) : 0;
    f.budget.value = String(Math.max(0, cur + dir * STEP));
    f.budget.dispatchEvent(new Event('input')); // «есть несохранённые изменения»
  };
  $('#b-dec').addEventListener('click', () => bump(-1));
  $('#b-inc').addEventListener('click', () => bump(1));
  f.budget.addEventListener('keydown', e => {
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
    e.preventDefault();
    bump(e.key === 'ArrowUp' ? 1 : -1);
  });
  els.key.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); $('#key-save').click(); }
  });

  const fail = text => { els.err.hidden = false; els.err.textContent = text; };

  // «Сохранить»: на сервер уходят ровно четыре поля. Ответ сервера — то, что записано
  // (адрес и модель обрезаны по краям), его и показываем.
  $('#save').addEventListener('click', () => run(async () => {
    els.err.hidden = true;
    els.backup.hidden = true;
    els.status.textContent = '';
    // Пустой бюджет нельзя молча считать нулём: 0 значит «без урезания».
    const raw = f.budget.value.trim();
    if (raw === '' || !Number.isFinite(Number(raw))) {
      fail('Бюджет контекста: введите число; 0 — без урезания.');
      return;
    }
    let saved;
    try {
      saved = await send('PUT', '/api/settings',
        { base_url: f.base.value, model: f.model.value, reasoning_effort: f.effort.value, context_budget: Number(raw) });
    } catch (e) {
      fail(reason(e)); // текст сервера как есть, поля остаются как набраны
      return;
    }
    fill(saved);
    paintKey(els.keyStatus, saved?.key);
    els.status.textContent = 'сохранено';
    const backup = saved?.backup ?? '';
    if (backup) { // один раз: следующее сохранение строку уберёт
      els.backup.textContent = `копия повреждённого файла сохранена рядом: ${backup}`;
      els.backup.hidden = false;
    }
    await refresh(); // шапка (модель, бюджет) и предупреждение о config.json — по новому состоянию
  }));

  // Итог действия над ключом. ok: true — удача, false — отказ (в рамке, как
  // пометка «нет в выдержках»), null — ещё идёт. Ключ в сообщение не попадает:
  // сервер его не повторяет, а мы пишем только его слова.
  const note = (ok, text) => {
    els.note.hidden = false;
    els.note.replaceChildren();
    const s = document.createElement('span');
    s.className = ok === false ? 'warn' : 'hint';
    s.textContent = text;
    els.note.append(s);
  };
  // После правки ключа состояние перечитывается: от него зависит подсказка в
  // диалоге. Строка ключа — по свежему состоянию, а нет его — по ответу действия.
  const afterKey = async fallback => {
    await refresh();
    paintKey(els.keyStatus, app.state?.key ?? fallback);
  };

  $('#key-save').addEventListener('click', () => run(async () => {
    els.note.hidden = true;
    try {
      const r = await send('PUT', '/api/key', { key: els.key.value });
      els.key.value = ''; // принят — из поля уходит; при отказе остаётся, чтобы не набирать заново
      note(true, 'ключ сохранён');
      await afterKey(r?.key);
    } catch (e) {
      note(false, reason(e));
    }
  }));

  $('#key-del').addEventListener('click', () => run(async () => {
    els.note.hidden = true;
    if (!confirm('Удалить сохранённый ключ API?')) return;
    try {
      await send('DELETE', '/api/key');
      note(true, 'ключ удалён');
      await afterKey({ set: false });
    } catch (e) {
      note(false, reason(e));
    }
  }));

  $('#key-check').addEventListener('click', () => run(async () => {
    note(null, 'проверяю…');
    try {
      const r = await send('POST', '/api/key/check');
      note(r?.ok ?? false, (r?.message ?? '') || 'ответ без пояснения');
    } catch (e) {
      note(false, reason(e));
    }
  }));
}

// ---- расход ----

const ruDate = s => String(s ?? '').split('-').reverse().join('.');

// Числа строки неполны, если среди её ответов были оборванные или без чисел
// от провайдера (partial): тогда «≈», а когда считать нечего — прочерк.
function tokenCell(r, n) {
  const any = ['input', 'output', 'cache_read'].some(k => (r[k] ?? 0) > 0);
  if ((r.partial ?? false) && !any) return '—';
  return ((r.partial ?? false) ? '≈' : '') + fmtTokens(n ?? 0);
}

const usageRow = (r, total) => `<tr${total ? ' class="total"' : ''}>
  <td>${total ? 'ИТОГО' : esc(ruDate(r.day))}</td><td>${total ? '' : esc((r.model ?? '') || '—')}</td>
  <td>${esc(tokenCell(r, r.input))}</td><td>${esc(tokenCell(r, r.output))}</td><td>${esc(tokenCell(r, r.cache_read))}</td></tr>`;

async function usageTab(box) {
  const p = pane(box);
  p.innerHTML = `<div class="row" id="range">
      <label class="hint" for="u-from">с</label><input type="date" id="u-from">
      <label class="hint" for="u-to">по</label><input type="date" id="u-to">
      <button class="btn ghost" id="u-show" type="button">Показать</button></div>
    <div id="u-out"></div>`;
  const from = p.querySelector('#u-from');
  const to = p.querySelector('#u-to');
  const show = p.querySelector('#u-show');
  const out = p.querySelector('#u-out');
  let busy = false;

  // load — таблица за период. Без дат сервер берёт текущий месяц; его границы и
  // подставляются в поля, так что период всегда виден.
  async function load() {
    if (busy) return;
    if (from.value && to.value && from.value > to.value) {
      out.innerHTML = '<div class="banner">начало периода позже его конца</div>';
      return;
    }
    busy = true;
    show.disabled = true;
    out.innerHTML = '<p class="hint">загружаю…</p>';
    try {
      const q = new URLSearchParams();
      if (from.value) q.set('from', from.value);
      if (to.value) q.set('to', to.value);
      let u;
      try {
        u = await get('/api/usage' + (q.size ? '?' + q : ''));
      } catch (e) {
        out.innerHTML = `<div class="banner">расход не загрузился: ${esc(reason(e))}</div>`;
        return;
      }
      from.value = u.from ?? from.value;
      to.value = u.to ?? to.value;
      const rows = u.rows ?? [];
      const total = u.total ?? {};
      // Итог без строк бывает: журнал не прочитался, и итог помечен неполным.
      // Тогда таблица с «≈» нужна, чтобы это было видно.
      const table = rows.length || (total.partial ?? false)
        ? `<table class="usage"><tr><th>день</th><th>модель</th><th>вход</th><th>выход</th><th>из кэша</th></tr>
            ${rows.map(r => usageRow(r, false)).join('')}${usageRow(total, true)}</table>` : '';
      out.innerHTML = `<p class="hint">${esc(ruDate(u.from))} — ${esc(ruDate(u.to))}. Вход — без кэша, «из кэша» — прочитано из кэша провайдера.
        Знак ≈ — сумма неполная: ответ оборвался, и провайдер не назвал точных чисел. Прочерк — данных нет.</p>
        ${table}${rows.length ? '' : '<p class="hint">расход за период — нет</p>'}`;
    } finally {
      busy = false;
      show.disabled = false;
    }
  }

  show.addEventListener('click', load);
  for (const i of [from, to]) {
    i.addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); load(); }
    });
  }
  await load();
}

export async function showSettings(center, app) {
  center.innerHTML = `<div class="settings"><div class="tabs">
      <button data-tab="settings" type="button">Настройки</button>
      <button data-tab="usage" type="button">Расход</button></div><div id="tab"></div></div>`;
  const box = center.querySelector('#tab');
  const tabs = { settings: () => settingsTab(box, app), usage: () => usageTab(box) };
  center.querySelectorAll('.tabs button').forEach(b => b.addEventListener('click', () => {
    center.querySelectorAll('.tabs button').forEach(x => x.classList.toggle('on', x === b));
    tabs[b.dataset.tab]();
  }));
  center.querySelector('.tabs button').click();
}
