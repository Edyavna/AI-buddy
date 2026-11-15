import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.MENTOR_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.MENTOR_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.MENTOR_APP_URL ?? 'http://localhost:5173';
const directory = 'tmp/mentor-markdown-browser';
await mkdir(directory, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.MENTOR_BROWSER });
const errors = [];
async function login(name) {
  const page = await browser.newPage({ viewport: { width: 360, height: 950 }, acceptDownloads: true });
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${origin}/login`);
  await page.getByRole('radio', { name }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');
  return page;
}
async function api(page, path, expected = 200, method = 'GET', data) {
  const response = await page.request.fetch(`${origin}/api/v1${path}`, { method, data, headers: { Origin: origin } });
  assert.equal(response.status(), expected, `${method} ${path}: ${await response.text()}`);
  return response.json();
}
async function terminal(page, id, status) {
  for (let i = 0; i < 150; i++) {
    const history = await api(page, `/conversations/${id}/messages`);
    if (history.at(-1)?.role === 'assistant' && history.at(-1).status === status) return history;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`No durable ${status} response`);
}
async function ask(page, text, status = 'completed') {
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(text);
  const sent = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/messages'));
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  const response = await sent;
  assert.equal(response.status(), 200);
  assert.ok(response.headers()['content-type'].startsWith('text/event-stream'));
  await page.waitForURL(url => url.pathname.startsWith('/mentor/chat/'));
  const id = new URL(page.url()).pathname.split('/').at(-1);
  return { id, history: await terminal(page, id, status) };
}
const links = page => page.getByRole('link', { name: 'Скачать .md', exact: true });
const versions = history => history.filter(message => message.role === 'assistant' && message.status === 'completed' && message.outcome === 'answered').map(message => message.text);
async function downloads(page, texts) {
  await links(page).nth(texts.length - 1).waitFor();
  assert.equal(await links(page).count(), texts.length);
  const files = [];
  for (let i = 0; i < texts.length; i++) {
    const started = page.waitForEvent('download');
    await links(page).nth(i).click();
    const download = await started;
    assert.equal(download.suggestedFilename(), 'instruction.md');
    const path = `${directory}/version-${i + 1}.md`;
    await download.saveAs(path);
    const bytes = await readFile(path);
    assert.deepEqual(bytes, Buffer.from(texts[i], 'utf8'));
    assert.equal(new TextDecoder('utf-8', { fatal: true }).decode(bytes), texts[i]);
    files.push(bytes);
  }
  return files;
}
try {
  const mentor = await login('Тестовый наставник');
  if (process.argv.includes('--verify')) {
    const state = JSON.parse(await readFile(`${directory}/state.json`, 'utf8'));
    await mentor.goto(`${origin}/mentor/chat/${state.id}`);
    await mentor.getByRole('heading', { name: 'Подготовка Markdown-инструкции' }).waitFor();
    assert.deepEqual(versions(await api(mentor, `/conversations/${state.id}/messages`)), state.versions);
    await downloads(mentor, state.versions);
    assert.equal((await api(mentor, `/mentor/documents/${state.document}`)).content, state.versions[1]);
    const peer = await login('Второй наставник');
    await api(peer, `/conversations/${state.id}/messages`, 404);
    console.log('Markdown versions, downloads, upload and ownership survived API/provider restart and repeated seed');
  } else {
    let uploads = 0;
    mentor.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/mentor/documents') uploads++; });
    await mentor.goto(`${origin}/mentor/documents`);
    const before = await api(mentor, '/mentor/documents');
    const generate = mentor.getByRole('link', { name: 'Сгенерировать Markdown' });
    await generate.focus();
    await mentor.keyboard.press('Enter');
    await mentor.getByRole('heading', { name: 'Подготовка Markdown-инструкции' }).waitFor();
    assert.equal(new URL(mentor.url()).searchParams.get('mode'), 'markdown');
    const first = await ask(mentor, 'N75 нужна инструкция');
    assert.equal(first.history.at(-1).outcome, 'needs_clarification');
    await mentor.getByText('Какой репозиторий использует команда?', { exact: true }).waitFor();
    assert.equal(await links(mentor).count(), 0);
    const conversation = await api(mentor, `/conversations/${first.id}`);
    assert.equal(conversation.markdown, true);
    await ask(mentor, 'N75 репозиторий team/repo');
    const second = await ask(mentor, 'N75 добавь пример');
    const initial = versions(second.history);
    assert.equal(initial.length, 2);
    assert.ok(initial[1].includes(initial[0]));
    await downloads(mentor, initial);
    await ask(mentor, 'N75 fail добавь отказ', 'failed');
    const retry = mentor.getByRole('button', { name: 'Повторить запрос', exact: true });
    await retry.waitFor();
    await downloads(mentor, initial);
    const sent = mentor.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/messages'));
    await retry.click();
    assert.equal((await sent).status(), 200);
    const history = await terminal(mentor, first.id, 'completed');
    assert.notEqual(history.at(-2).client_message_id, history.at(-4).client_message_id);
    const texts = versions(history);
    assert.equal(texts.length, 3);
    assert.deepEqual(texts.slice(0, 2), initial);
    await mentor.reload();
    await mentor.getByRole('heading', { name: 'Подготовка Markdown-инструкции' }).waitFor();
    await downloads(mentor, texts);
    for (const width of [360, 768, 1440]) {
      await mentor.setViewportSize({ width, height: 950 });
      assert.equal(await mentor.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `overflow at ${width}`);
      await mentor.screenshot({ path: `${directory}/markdown-${width}.png`, fullPage: true, animations: 'disabled' });
    }
    const row = mentor.locator('[class*="conversationRow"]').filter({ has: mentor.locator(`a[href="/mentor/chat/${first.id}"]`) });
    await row.getByRole('button', { name: /^Архивировать диалог:/ }).focus();
    await mentor.keyboard.press('Enter');
    await row.waitFor({ state: 'hidden' });
    await downloads(mentor, texts);
    await mentor.getByRole('button', { name: 'Архив', exact: true }).click();
    await row.waitFor();
    await row.getByRole('button', { name: /^Вернуть из архива:/ }).focus();
    await mentor.keyboard.press('Enter');
    await row.waitFor({ state: 'hidden' });
    assert.equal((await api(mentor, `/conversations/${first.id}`)).markdown, true);
    const peer = await login('Второй наставник');
    await peer.goto(`${origin}/mentor/chat/${first.id}`);
    await peer.getByRole('alert').first().waitFor();
    assert.equal(await links(peer).count(), 0);
    for (const suffix of ['', '/messages']) await api(peer, `/conversations/${first.id}${suffix}`, 404);
    await api(peer, `/conversations/${first.id}`, 404, 'PATCH', { archived: true });
    await api(peer, `/conversations/${first.id}/messages`, 404, 'POST', {});
    assert.ok(!(await api(peer, '/conversations')).some(item => item.id === first.id));
    const intern = await login('Тестовый стажер');
    await api(intern, '/conversations', 403, 'POST', { client_conversation_id: crypto.randomUUID(), markdown: true });
    await api(intern, `/conversations/${first.id}/messages`, 404);
    assert.equal(uploads, 0, 'Generation published a document without confirmation');
    assert.equal((await api(mentor, '/mentor/documents')).items.length, before.items.length);
    await mentor.goto(`${origin}/mentor/documents`);
    await mentor.getByRole('textbox', { name: 'Название документа' }).fill(`N75 выбранная версия ${first.id}`);
    await mentor.getByLabel('Файл Markdown', { exact: true }).setInputFiles({ name: 'instruction.md', mimeType: 'text/markdown', buffer: Buffer.from(texts[1], 'utf8') });
    assert.equal(uploads, 0);
    const uploaded = mentor.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/mentor/documents' && response.request().method() === 'POST');
    await mentor.getByRole('button', { name: 'Загрузить документ', exact: true }).click();
    const response = await uploaded;
    assert.equal(response.status(), 202);
    const document = await response.json();
    assert.equal(uploads, 1);
    let saved;
    for (let i = 0; i < 150; i++) {
      saved = await api(mentor, `/mentor/documents/${document.id}`);
      if (saved.status === 'ready') break;
      assert.notEqual(saved.status, 'failed');
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.equal(saved.status, 'ready');
    assert.equal(saved.content, texts[1]);
    await writeFile(`${directory}/state.json`, JSON.stringify({ id: first.id, versions: texts, document: document.id }));
    console.log('Markdown clarification, versions, UTF-8 downloads, model retry, archive, isolation and explicit upload passed at 360/768/1440');
  }
  assert.deepEqual(errors, [], 'Browser page errors');
} catch (error) {
  for (const [index, context] of browser.contexts().entries()) for (const page of context.pages()) {
    console.error(page.url(), (await page.locator('body').innerText()).slice(0, 2200));
    await page.screenshot({ path: `${directory}/failure-${index}.png`, fullPage: true });
  }
  throw error;
} finally { await browser.close(); }
