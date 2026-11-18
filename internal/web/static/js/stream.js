// Поток событий ответа. EventSource умеет только GET, а ход — это POST с
// телом, поэтому поток читается из fetch вручную: события разделены пустой
// строкой, в каждом — строки «event:» и «data:».
//
// Сбой в разборе или в обработчике события не должен оставлять ход
// работать вхолостую: пока соединение открыто, сервер дописывает ответ и держит
// беседу занятой (следующий вопрос получил бы 409). Поэтому читатель снимается —
// это закрывает соединение, сервер видит уход клиента и останавливает ход, — и
// ошибка идёт вызывающему.
export async function readSSE(response, onEvent) {
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf('\n\n')) >= 0) {
        const raw = buf.slice(0, i);
        buf = buf.slice(i + 2);
        let event = 'message';
        let data = '';
        for (const line of raw.split('\n')) {
          if (line.startsWith('event:')) event = line.slice(6).trim();
          else if (line.startsWith('data:')) data += line.slice(5).trim();
        }
        if (data) onEvent(event, JSON.parse(data));
      }
    }
  } catch (e) {
    await reader.cancel().catch(() => {});
    throw e;
  }
}
