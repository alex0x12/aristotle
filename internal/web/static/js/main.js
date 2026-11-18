import { get, send, esc, fmtK, fmtTokens, answerTokens } from './api.js';
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
  convs: [],
  currentId: '',
  lastUsed: 0,
  // Потолок входа запроса в токенах; 0 — без урезания. Берётся из состояния и
  // обновляется событием usage (сервер называет в нём бюджет того хода).
  contextBudget: 0,
  answerLine: '',
  convLine: '',

  budget() { return this.contextBudget; },

  async refreshState() {
    this.state = await get('/api/state');
    // Бюджет приходит числом, но шапка не должна ломаться, если поля нет или оно
    // не число: тогда считаем «без урезания», а не показываем NaN.
    const b = Number(this.state?.context_budget);
    this.contextBudget = b > 0 ? b : 0;
    this.paintModel();
    this.onContext(this.lastUsed); // шкала — по новому бюджету
    this.paintMonth();
    this.paintDegraded();
  },

  // paintModel — имя модели в шапке простым текстом: выбирать там больше нечего,
  // модель одна, она меняется в настройках.
  paintModel() {
    const el = document.getElementById('model');
    el.textContent = this.state?.model ?? '';
    el.hidden = !el.textContent;
  },

  // paintDegraded — постоянное предупреждение над центральной панелью, пока
  // сервер работает без читаемого config.json (на настройках по умолчанию).
  // Текст — сервера; textContent, а не разметка.
  paintDegraded() {
    const box = document.getElementById('degraded');
    const on = !!(this.state?.degraded ?? false);
    box.hidden = !on;
    box.textContent = on ? (this.state?.warning ?? '') || 'config.json не читается' : '';
  },

  // paintMonth — итог месяца подсказкой к строке расхода в шапке, в токенах.
  // «≈» — в нём есть оборванные ответы, сумма неполная.
  paintMonth() {
    const m = this.state?.month;
    const line = document.getElementById('usage-line');
    if (!m?.answers) { line.title = 'за месяц: расхода нет'; return; }
    const tokens = (m.input ?? 0) + (m.output ?? 0) + (m.cache_read ?? 0);
    const partial = m.partial ?? false;
    line.title = 'за месяц: ' + (tokens || !partial ? (partial ? '≈' : '') + fmtTokens(tokens) + ' ток.' : '—');
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

  // paintUsage — вторая строка шапки: ответ и беседа, только токены.
  paintUsage() {
    document.getElementById('usage-line').textContent =
      [this.answerLine, this.convLine].filter(Boolean).join(' · ');
  },

  // onContext — шкала контекста и итоги беседы в шапке. tokens не передан —
  // меняется только шкала (например, после смены бюджета в настройках).
  onContext(used, tokens) {
    used ??= 0;
    this.lastUsed = used;
    const budget = this.budget();
    const bar = document.getElementById('ctx-bar');
    // Бюджет 0 — не «нулевой потолок», а «не урезать»: модели уходит вся беседа.
    // Деления на ноль нет, шкала не рисуется (у неё нет конца), а подпись говорит
    // об этом словами.
    const shown = used ? fmtK(used) : '—';
    document.getElementById('ctx-label').textContent =
      budget ? `контекст ${shown} / ${fmtK(budget)}` : `контекст ${shown} · без урезания`;
    bar.hidden = !budget;
    bar.querySelector('i').style.width = budget ? Math.min(100, 100 * used / budget) + '%' : '0';
    // Не «over»: так уже зовётся наложение на фриз.
    bar.classList.toggle('exceeded', budget > 0 && used > budget);
    if (tokens !== undefined) {
      this.convLine = tokens ? `беседа ${fmtTokens(tokens)} ток.` : '';
      this.answerLine = '';
      this.paintUsage();
    }
  },

  // onUsage — событие usage потока: {usage, context_used, context_budget,
  // conversation_tokens}. Бюджет в нём — тот, по которому сервер собрал ход.
  onUsage(d) {
    const b = Number(d.context_budget);
    if (d.context_budget != null && b >= 0) this.contextBudget = b;
    this.onContext(d.context_used, d.conversation_tokens);
    this.answerLine = answerTokens(d.usage);
    this.paintUsage();
  },

  // onDone — ход закончился: список бесед (порядок, название) обновляется здесь,
  // итог месяца и состояние ключа — в refreshState, который зовёт диалог.
  onDone() {
    this.refreshConvs();
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
// Не вышло состояние (модель, шапка) — список бесед и диалог всё равно
// должны открыться: они от него не зависят.
try {
  await app.refreshState();
} catch (e) {
  console.error('состояние не загрузилось', e);
  const box = document.getElementById('degraded');
  box.hidden = false;
  box.textContent = 'состояние не загрузилось — шапка может быть неполной';
}
await app.refreshConvs();
route();
