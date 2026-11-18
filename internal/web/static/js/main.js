import { get, send, esc, fmtK, fmtTokens, fmtCost, answerTokens } from './api.js';
import { showDialog } from './dialog.js';

// Экраны библиотеки и настроек подгружаются по требованию (задачи 9 и 10).
const lazy = name => import(`./${name}.js`);

export const app = {
  state: null,
  profile: '',
  convs: [],
  currentId: '',
  lastUsed: 0,
  answerLine: '',
  convLine: '',

  budget() {
    const p = this.state?.profiles.find(x => x.name === (this.profile || this.state.default));
    return p?.context_budget ?? 0;
  },

  async refreshState() {
    this.state = await get('/api/state');
    const sel = document.getElementById('profile');
    sel.innerHTML = this.state.profiles.map(p =>
      `<option value="${esc(p.name)}">${esc(p.name)} · ${esc(p.model)}</option>`).join('');
    sel.value = this.profile || this.state.default;
    sel.onchange = () => { this.profile = sel.value; this.onContext(this.lastUsed); };
  },

  async refreshConvs() {
    this.convs = await get('/api/conversations');
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

  onDone() { this.refreshConvs(); },
};

// screen — экран из отдельного модуля; не загрузился — сказать об этом, а не
// оставить на месте прежний.
async function screen(center, name, show) {
  try {
    return await show(await lazy(name));
  } catch (e) {
    center.innerHTML = `<div class="dialog"><p class="status">экран не открылся: ${esc(e.message)}</p></div>`;
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
await app.refreshState();
await app.refreshConvs();
route();
