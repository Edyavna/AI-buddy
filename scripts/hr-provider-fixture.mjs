import { createServer } from 'node:http';

const port = Number(process.env.HR_FIXTURE_PORT ?? 55662);
const vector = Array(1024).fill(0);
vector[0] = 1;

createServer(async (request, response) => {
  response.setHeader('Content-Type', 'application/json');
  try {
    if (request.method === 'GET' && request.url === '/ready') {
      response.end('{}');
      return;
    }
    let raw = '';
    for await (const part of request) raw += part;
    const input = JSON.parse(raw);
    if (request.url === '/embeddings') {
      response.end(JSON.stringify({ data: input.input.map((_, index) => ({ index, embedding: vector })) }));
      return;
    }
    if (request.url !== '/chat/completions') throw new Error('Unknown path');
    const last = input.messages.at(-1).content;
    // #57 exercises genuine read tools without changing other fixture scenarios.
    const userIndex = input.messages.findLastIndex(message => message.role === 'user');
    const currentQuestion = input.messages[userIndex]?.content ?? '';
    if (input.tools?.length && /N57/.test(currentQuestion)) {
      const turn = input.messages.slice(userIndex + 1);
      const replies = turn.filter(message => message.role === 'tool');
      const call = (id, name, args) => response.end(JSON.stringify({ choices: [{ finish_reason: 'tool_calls', message: { role: 'assistant', content: '', tool_calls: [{ id, type: 'function', function: { name, arguments: JSON.stringify(args) } }] } }] }));
      if (!replies.length) { call('context-search', 'search_knowledge', { query: currentQuestion }); return; }
      const sources = JSON.parse(replies[0].content);
      if (sources.length && replies.length === 1) { call('context-read', 'read_document', { document_id: sources[0].DocumentID }); return; }
      response.end(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { role: 'assistant', content: JSON.stringify({ answer: 'Проверьте конфигурацию по доступной инструкции. При необходимости уточните порядок у наставника.', outcome: sources.length ? 'answered' : 'knowledge_gap' }) } }] }));
      return;
    }
    let result = { answer: 'Порядок работы можно уточнить у наставника.', outcome: 'answered' };
    if (input.response_format?.type === 'json_object' && !input.tools?.length) {
      if (input.messages[0].content.startsWith('Подготовь обезличенную')) {
        result = { title: 'Инструкция команды', content: JSON.parse(last).answer };
      } else if (input.messages[0].content.startsWith('Определи тему')) {
        const hr = /отпуск|кадров|трудов/i.test(last);
        result = { direction: hr ? 'hr' : 'mentor', reason: hr ? 'Вопрос об отпуске' : 'Рабочий вопрос', clarification: null, split: false };
      } else {
        const payload = JSON.parse(last);
        result = { answer: 'Подайте заявление через кадровый портал.', document_id: payload.documents[0].ID };
      }
    }
    response.end(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { role: 'assistant', content: JSON.stringify(result) } }] }));
  } catch (error) {
    console.error(error.message);
    response.statusCode = 400;
    response.end('{}');
  }
}).listen(port, '127.0.0.1');
