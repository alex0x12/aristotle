import { get, send, esc, fmtK, fmtTokens, fmtCost, answerTokens } from './api.js';
import { showDialog } from './dialog.js';

// Экраны библиотеки и настроек подгружаются по требованию. Удавшаяся загрузка
// запоминается здесь, а не только в браузере: после повтора с меткой (см.
// lazy) обычный адрес модуля остался бы за ошибкой.
const loaded = new Map();

// retry — повторная попытка после неудачи. Модульная карта HTML вправе помнить
// неудавшийся import по адресу и отвечать ту же ошибку, даже когда файл уже
// есть. Chromium (проверено 18.11.26) повторяет сам, Firefox и Safari не
// проверялись; метка в запросе делает адрес новым, и загрузка идёт заново в
// любом из них.
async function lazy(name, retry) {
  if (loaded.has(name)) return loaded.get(name);
  const mod = await import(retry ? `./${name}.js?retry=${Date.now()}` : `./${name}.js`);
  loaded.set(name, mod);
  return mod;
}

export const app = {
  state: null,
  profile: '',
  convs: [],
  currentId: '',
  lastUsed: 0,
  answerLine: '',
  convLine: '',

  budget() {
    const p = this.state?.profiles?.find(x => x.name === (this.profile || this.state.default));
    return p?.context_budget ?? 0;
  },

  async refreshState() {
    this.state = await get('/api/state');
    const sel = document.getElementById('profile');
    sel.innerHTML = (this.state.profiles ?? []).map(p =>
      `<option value="${esc(p.name)}">${esc(p.name)} · ${esc(p.model)}</option>`).join('');
    // Профиль, убранный в настройках, больше не выбран: ход с ним был бы отказом.
    if (!this.state.profiles?.some(p => p.name === this.profile)) this.profile = '';
    sel.value = this.profile || this.state.default;
    sel.onchange = () => { this.profile = sel.value; this.onContext(this.lastUsed); };
    this.paintMonth();
    this.paintDegraded();
  },

  // paintDegraded — постоянное предупреждение над центральной панелью, пока
  // сервер работает без config.json (встроенные профили). Текст — сервера;
  // textContent, а не разметка.
  paintDegraded() {
    const box = document.getElementById('degraded');
    const on = !!(this.state?.degraded ?? false);
    box.hidden = !on;
    box.textContent = on ? (this.state?.warning ?? '') || 'config.json не читается' : '';
  },

  // paintMonth — итог месяца подсказкой к строке расхода в шапке. «≈» — в нём
  // есть оборванные ответы или ответы без цены, сумма неполная.
  paintMonth() {
    const m = this.state?.month;
    const line = document.getElementById('usage-line');
    if (!m?.answers) { line.title = 'за месяц: расхода нет'; return; }
    const tokens = (m.input ?? 0) + (m.output ?? 0) + (m.cache_read ?? 0) + (m.cache_write ?? 0);
    const text = tokens || !m.partial ? (m.partial ? '≈' : '') + fmtTokens(tokens) + ' ток.' : '—';
    const cost = m.cost_usd != null ? ' · ' + (m.partial || m.cost_partial ? '≈' : '') + fmtCost(m.cost_usd) : '';
    line.title = 'за месяц: ' + text + cost;
  },

  async refreshConvs() {
    this.convs = (await get('/api/conversations')) ?? [];
    const box = document.getElementById('convs');
    box.innerHTML = '';
    for (const c of this.convs) {
      const row = document.createElement('a');
      row.className = 'conv' + (c.id === this.currentId ? ' on' : '');
      row.href = '#/c/' + c.id;
      row.title = 'Двойной щелчок — переименовать';
      row.innerHTML = `<span class="t">${esc(c.title)}</span><button class="del" type="button" title="Удалить">×</button>`;
      row.addEventListener('dblclick', async e => {
        e.preventDefault();
        const title = prompt('Название беседы', c.title);
        if (title?.trim()) { await send('PATCH', '/api/conversations/' + c.id, { title }); this.refreshConvs(); }
      });
      row.querySelector('.del').addEventListener('click', async e => {
        e.preventDefault(); e.stopPropagation();
        if (!confirm(`Удалить беседу «${c.title}»? Журнал уйдёт в корзину.`)) return;
        await send('DELETE', '/api/conversations/' + c.id);
        await this.refreshConvs();
        if (c.id === this.currentId) location.hash = '#/';
      });
      box.append(row);
    }
  },

  // paintUsage — вторая строка шапки: ответ, беседа, цена.
  paintUsage() {
    document.getElementById('usage-line').textContent =
      [this.answerLine, this.convLine].filter(Boolean).join(' · ');
  },

  // onContext — шкала контекста и итоги беседы в шапке. tokens не передан —
  // меняется только шкала (например, при смене профиля).
  onContext(used, tokens, cost) {
    used ??= 0;
    this.lastUsed = used;
    const budget = this.budget();
    const bar = document.getElementById('ctx-bar');
    document.getElementById('ctx-label').textContent =
      budget ? `контекст ${used ? fmtK(used) : '—'} / ${fmtK(budget)}` : 'контекст без ограничения';
    bar.querySelector('i').style.width = budget ? Math.min(100, 100 * used / budget) + '%' : '0';
    // Не «over»: так уже зовётся наложение на фриз.
    bar.classList.toggle('exceeded', budget > 0 && used > budget);
    if (tokens !== undefined) {
      this.convLine = tokens ? `беседа ${fmtTokens(tokens)} ток.` + (cost != null ? ' · ' + fmtCost(cost) : '') : '';
      this.answerLine = '';
      this.paintUsage();
    }
  },

  onUsage(d) {
    this.onContext(d.context_used, d.conversation_tokens, d.conversation_cost_usd);
    this.answerLine = answerTokens(d.usage);
    this.paintUsage();
  },

  async onDone() {
    this.refreshConvs();
    // Итог месяца вырос; подсказку обновляем, не трогая выбор профиля в шапке.
    try { this.state.month = (await get('/api/state')).month; this.paintMonth(); } catch { /* шапка подождёт */ }
  },
};

// screen — экран из отдельного модуля; не загрузился — сказать об этом
// спокойно, а не оставить на месте прежний. Текст ошибки браузера
// («error loading dynamically imported module …») человеку ничего не даёт и в
// страницу не попадает — только в консоль. «повторить» грузит модуль заново.
async function screen(center, name, show, retry = false) {
  try {
    return await show(await lazy(name, retry));
  } catch (e) {
    console.error(`экран «${name}» не открылся`, e);
    center.innerHTML = `<div class="dialog"><p class="status">экран не открылся</p>
      <div class="row"><button class="btn ghost" id="retry" type="button">повторить</button></div></div>`;
    const btn = center.querySelector('#retry');
    btn.addEventListener('click', () => { btn.disabled = true; screen(center, name, show, true); });
  }
}

async function route() {
  const center = document.getElementById('center');
  const h = location.hash || '#/';
  document.getElementById('src').hidden = true;
  let m;
  if ((m = h.match(/^#\/c\/([\dT-]+)$/))) {
    app.currentId = m[1];
    app.refreshConvs();
    return showDialog(center, m[1], app);
  }
  app.currentId = '';
  if ((m = h.match(/^#\/library(?:\/(.+))?$/))) {
    return screen(center, 'library', mod => mod.showLibrary(center, m[1] ? decodeURI(m[1]) : ''));
  }
  if (h.startsWith('#/settings')) {
    return screen(center, 'settings', mod => mod.showSettings(center, app));
  }
  // Корень — новая беседа с приветствием; файл журнала появится с первым вопросом.
  const { id } = await send('POST', '/api/conversations');
  location.replace('#/c/' + id);
}

// Смена адреса сама вызовет route() через hashchange; второй вызов создал бы
// лишнюю беседу.
document.getElementById('new-conv').addEventListener('click', () => {
  if (location.hash === '#/' || !location.hash) route(); else location.hash = '#/';
});
window.addEventListener('hashchange', route);
// Не вышло состояние (профили, шапка) — список бесед и диалог всё равно
// должны открыться: они от него не зависят.
try {
  await app.refreshState();
} catch (e) {
  console.error('состояние не загрузилось', e);
  const box = document.getElementById('degraded');
  box.hidden = false;
  box.textContent = 'состояние не загрузилось — профили и шапка могут быть неполными';
}
await app.refreshConvs();
route();
