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
