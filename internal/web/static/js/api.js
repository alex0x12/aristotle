// Обращения к серверу. Ошибка сервера приходит текстом — его и показываем.
async function check(res) {
  if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
  return res;
}

export async function get(path) {
  return (await check(await fetch(path))).json();
}

export async function send(method, path, body) {
  const res = await check(await fetch(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  }));
  return res.status === 204 ? null : res.json();
}

const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
export function esc(s) { return String(s ?? '').replace(/[&<>"']/g, c => ESC[c]); }

export function fmtTokens(n) { return (n ?? 0).toLocaleString('ru-RU'); }
export function fmtK(n) { return n >= 1000 ? (n / 1000).toFixed(1).replace('.', ',').replace(',0', '') + 'k' : String(n ?? 0); }
// cost_usd бывает null или вовсе отсутствует (цены профиля не заданы) — тогда
// цены нет и «$» не показывается.
export function fmtCost(c) { return c == null ? '' : '$' + c.toFixed(c < 1 ? 3 : 2).replace('.', ','); }

const COUNTS = ['input', 'output', 'cache_read', 'cache_write'];

// Расход ответа известен точно только при known: true. Иначе это сумма
// завершённых запросов хода: ответ прервался, и недостающего провайдер не
// сообщил. Такую сумму нельзя выдавать за точную — она с «≈»; а если в ней
// одни нули, то считать нечего — прочерк.
export function usageIsPartial(u) { return !u?.known && COUNTS.some(k => u?.[k]); }

export function usageText(u, cost) {
  u ??= {}; // usage в записи журнала может отсутствовать (omitempty)
  if (!u.known && !usageIsPartial(u)) return 'токены: —';
  const pre = u.known ? '' : '≈';
  const parts = [`вход ${pre}${fmtTokens(u.input)}`];
  if (u.cache_read) parts.push(`кэш ${pre}${fmtTokens(u.cache_read)}`);
  parts.push(`вывод ${pre}${fmtTokens(u.output)}`);
  // Цена по неточной сумме тоже была бы неточной; сервер её и не присылает.
  if (u.known && cost != null) parts.push(fmtCost(cost));
  return parts.join(' · ');
}

// Токены ответа для шапки; пустая строка, если нечего показать.
export function answerTokens(u) {
  u ??= {};
  if (u.known) return `ответ ${fmtTokens(u.output)} ток.`;
  return usageIsPartial(u) && u.output ? `ответ ≈${fmtTokens(u.output)} ток.` : '';
}
