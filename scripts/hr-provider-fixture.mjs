import { createServer } from 'node:http';

const port = Number(process.env.HR_FIXTURE_PORT ?? 55662);
const vector = Array(1024).fill(0);
const mentorFailures = new Set();
const markdownFailures = new Set();
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
    if(input.tools?.length && /N79/.test(currentQuestion)) {
      const profile=input.messages.find(message=>message.role==='user'&&message.content.startsWith('Ниже персональный Markdown-профиль:'))?.content??'';
      const mentor=profile.match(/Действующий наставник: "([^"\n]+)"/)?.[1]??'не назначен';
      const answer=/наставник/i.test(currentQuestion)?`Текущий наставник: ${mentor}`:profile.includes('Использую PowerShell')?'В PowerShell проверьте текущую папку: `Get-Location`.':profile.includes('Использую Bash')?'В Bash проверьте текущую папку: `pwd`.':/использую PowerShell/i.test(currentQuestion)?'Учту это в текущем диалоге. Предпочтение можно подтвердить ниже для следующих разговоров.':'Уточните рабочую оболочку; предпочтения пока не сохранены.';
      response.end(JSON.stringify({choices:[{finish_reason:'stop',message:{role:'assistant',content:JSON.stringify({answer,outcome:'answered'})}}]}));return;
    }
    if (input.messages[0].content.startsWith('Ты помогаешь наставнику подготовить инструкцию')) {
      if (input.tools?.length) throw new Error('Markdown generation must not receive intern tools');
      const previous = input.messages.findLast(message => message.role === 'assistant' && message.content.startsWith('# Доступ к репозиторию'))?.content;
      let answer = '# Доступ к репозиторию\n\nКоманда использует team/repo.\n\n```powershell\ngit clone https://example.org/team/repo.git\n```\n';
      let clarify = false;
      if (currentQuestion === 'N75 нужна инструкция') {
        answer = 'Какой репозиторий использует команда?'; clarify = true;
      } else if (currentQuestion === 'N75 добавь пример') {
        if (!previous) throw new Error('Previous Markdown version missing from model history');
        answer = previous + '\n## Пример для стажёра\n\nПопросите у наставника доступ к team/repo.\n';
      } else if (currentQuestion === 'N75 fail добавь отказ') {
        if (!previous?.includes('## Пример для стажёра')) throw new Error('Updated Markdown version missing from model history');
        if (!markdownFailures.has(currentQuestion)) {
          markdownFailures.add(currentQuestion); response.statusCode = 503; response.end('{}'); return;
        }
        answer = previous + '\n## При отказе\n\nУточните причину у наставника.\n';
      }
      response.end(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { role: 'assistant', content: JSON.stringify({ answer, clarify }) } }] }));
      return;
    }
    if (input.messages[0].content.startsWith('Ты помощник наставника.')) {
      if (input.tools?.length) throw new Error('Mentor chat must not receive intern tools');
      if (currentQuestion.includes('N76 fail') && !mentorFailures.has(currentQuestion)) {
        mentorFailures.add(currentQuestion); response.statusCode = 503; response.end('{}'); return;
      }
      const history = input.messages.filter(message => message.role === 'user').map(message => message.content).join(' | ');
      response.end(JSON.stringify({ choices: [{ finish_reason: 'stop', message: { role: 'assistant', content: JSON.stringify({ answer: `Ответ наставнику: ${history}` }) } }] }));
      return;
    }
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
      if(input.messages[0].content.startsWith('Придумай короткое название диалога') && /N79/.test(last)) {
        result={title:'Личный диалог'};
      } else if(input.messages[0].content.startsWith('Предложи факты для персональной памяти')) {
        const payload=JSON.parse(last);
        // The full source includes the browser marker and any surrounding context.
        if(/изучаю Docker/i.test(payload.text)) {response.statusCode=503;response.end('{}');return;}
        const facts=[];
        const ambiguous=payload.text.match(/у коллеги Windows, мне тоже удобно/i);
        if(ambiguous)facts.push({category:'work_environment',key:'os',value:'windows',source_quote:ambiguous[0],uncertain:true});
        for(const option of payload.catalog) {
          const match=payload.text.match(new RegExp(option.text.replace(/[.*+?^${}()|[\]\\]/g,'\\$&'),'i'));
          if(match&&facts.length<4)facts.push({category:option.category,key:option.key,value:option.value,source_quote:match[0],uncertain:false});
        }
        result={facts};
      } else if (input.messages[0].content.startsWith('Подготовь обезличенную')) {
        result = { title: 'Инструкция команды', content: JSON.parse(last).answer };
      } else if (input.messages[0].content.startsWith('Определи тему')) {
        const company = input.messages[0].content.includes('direction="company"') && /Когда основали Авито/i.test(last);
        const hr = /отпуск|кадров|трудов/i.test(last);
        result = { direction: hr ? 'hr' : 'mentor', reason: hr ? 'Вопрос об отпуске' : 'Рабочий вопрос', clarification: null, split: false };
        if (company) result = { direction: 'company', reason: 'История Авито', clarification: null, split: false };
      } else if (input.messages[0].content.includes('редакции статьи из Википедии')) {
        const { Excerpts } = JSON.parse(last);
        if (!Excerpts.some(text => text.includes('2007') && text.includes('Йонас'))) throw new Error('Missing indexed Avito history');
        result = { answer: 'Авито основали в 2007 году Йонас Нордландер и Филип Энгельберт.' };
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
