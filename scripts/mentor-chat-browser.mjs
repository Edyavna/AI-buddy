import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.MENTOR_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.MENTOR_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.MENTOR_APP_URL ?? 'http://localhost:5173';
const directory = 'tmp/mentor-chat-browser';
const firstQuestion = 'N76 ЛИПА: запомни кодовое слово для этого разговора';
const followUp = 'N76 какое кодовое слово я назвал?';
const browser = await chromium.launch({ headless: true, executablePath: process.env.MENTOR_BROWSER });
const errors = [];
await mkdir(directory, { recursive: true });
async function login(name, current) {
  const page = current ?? await browser.newPage({ viewport: { width: 360, height: 950 } });
  if (!current) page.on('pageerror', error => errors.push(error.message));
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
const answers = page => page.locator('[role="log"] article').filter({ has: page.getByText('AI-buddy', { exact: true }) });
async function terminalHistory(page, id, status) {
  for (let attempt = 0; attempt < 150; attempt++) {
    const history = await api(page, `/conversations/${id}/messages`);
    if (history.at(-1)?.role === 'assistant' && history.at(-1).status === status) return history;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`No durable ${status} reply in ${id}`);
}
async function ask(page, question, failure = false, mentor = true) {
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(question);
  const sent = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/messages'));
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  const response = await sent;
  assert.equal(response.status(), 200);
  assert.ok(response.headers()['content-type'].startsWith('text/event-stream'));
  const prefix = mentor ? '/mentor/chat/' : '/chat/';
  await page.waitForURL(url => url.pathname.startsWith(prefix));
  const id = new URL(page.url()).pathname.split('/').at(-1);
  const history = await terminalHistory(page, id, failure ? 'failed' : 'completed');
  if (mentor) {
    assert.deepEqual(history.at(-1).request_ids, [], 'Intern request in mentor reply');
    assert.deepEqual(history.at(-1).sources, [], 'Intern knowledge source in mentor reply');
    assert.ok(new URL(page.url()).pathname.startsWith('/mentor/chat/'));
  }
  await page.getByRole('button', { name: 'Отправить', exact: true }).waitFor();
  return id;
}
async function picture(page, label, width) {
  await page.setViewportSize({ width, height: 950 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${label} overflow at ${width}`);
  if (label === 'mentor') {
    assert.equal(await page.locator('header').first().evaluate(header => {
      const box = header.getBoundingClientRect();
      return [...header.querySelectorAll('[class*="userName"], [class*="userRole"]')].every(element => {
        const rect = element.getBoundingClientRect();
        return rect.height === 0 || (rect.top >= box.top && rect.bottom <= box.bottom + 1);
      });
    }), true, `Mentor identity overflows header at ${width}`);
  }
  await page.screenshot({ path: `${directory}/${label}-${width}.png`, fullPage: true, animations: 'disabled' });
}

try {
  if (process.argv.includes('--verify')) {
    const state = JSON.parse(await readFile(`${directory}/state.json`, 'utf8'));
    const mentor = await login('Тестовый наставник');
    await mentor.goto(`${origin}/mentor/chat/${state.first}`);
    await answers(mentor).last().getByText(followUp, { exact: false }).waitFor();
    const history = await api(mentor, `/conversations/${state.first}/messages`);
    assert.equal(history.length, 4);
    assert.ok(history.at(-1).text.includes(firstQuestion));
    assert.equal((await api(mentor, `/conversations/${state.retry}/messages`)).at(-1).status, 'completed');
    const peer = await login('Второй наставник');
    await api(peer, `/conversations/${state.first}`, 404);
    const intern = await login('Тестовый стажер');
    assert.equal((await api(intern, `/conversations/${state.intern}`)).step_id, 'step_local_setup');
    console.log('Mentor history, retry and ownership survived Go restart and repeated seed');
  } else {
    const mentor = await login('Тестовый наставник');
    await mentor.getByRole('link', { name: 'Чат', exact: true }).click();
    await mentor.getByRole('heading', { name: 'Помощник наставника' }).waitFor();
    const internRequests = [];
    mentor.on('request', request => {
      if (/\/api\/v1\/(onboarding|access-requests)(\/|$)/.test(new URL(request.url()).pathname)) internRequests.push(request.url());
    });
    await mentor.goto(`${origin}/mentor/chat?stepId=step_local_setup&direction=hr`);
    await mentor.getByRole('heading', { name: 'Помощник наставника' }).waitFor();
    assert.equal(await mentor.getByRole('link', { name: 'Открыть мой план' }).count(), 0);
    const first = await ask(mentor, firstQuestion);
    assert.equal((await api(mentor, `/conversations/${first}`)).step_id, null);
    await answers(mentor).last().getByText(firstQuestion, { exact: false }).waitFor();
    await ask(mentor, followUp);
    await answers(mentor).last().getByText(followUp, { exact: false }).waitFor();
    assert.ok((await answers(mentor).last().innerText()).includes(firstQuestion));
    await mentor.reload();
    await answers(mentor).last().getByText(followUp, { exact: false }).waitFor();
    assert.equal(await mentor.getByRole('button', { name: 'Обратиться к специалисту' }).count(), 0);
    for (const width of [360, 768, 1440]) await picture(mentor, 'mentor', width);
    const conversationLink = mentor.locator(`a[href="/mentor/chat/${first}"]`);
    const row = mentor.locator('[class*="conversationRow"]').filter({ has: conversationLink });
    await row.getByRole('button', { name: `Архивировать диалог: ${firstQuestion}` }).focus();
    await mentor.keyboard.press('Enter');
    await conversationLink.waitFor({ state: 'hidden' });
    await mentor.getByRole('button', { name: 'Архив', exact: true }).click();
    const archived = conversationLink;
    await archived.waitFor();
    assert.equal(await archived.getAttribute('href'), `/mentor/chat/${first}`);
    await row.getByRole('button', { name: `Вернуть из архива: ${firstQuestion}` }).focus();
    await mentor.keyboard.press('Enter');
    await archived.waitFor({ state: 'hidden' });
    await mentor.getByRole('button', { name: 'Активные диалоги', exact: true }).click();
    await mentor.getByRole('link', { name: 'Новый диалог', exact: true }).click();
    await mentor.waitForURL(url => url.pathname === '/mentor/chat');
    const retry = await ask(mentor, 'N76 fail одноразовый сбой', true);
    const repeat = mentor.getByRole('button', { name: 'Повторить запрос', exact: true });
    await repeat.waitFor();
    const retried = mentor.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/messages'));
    await repeat.focus();
    await mentor.keyboard.press('Enter');
    const retryResponse = await retried;
    assert.equal(retryResponse.status(), 200);
    const retryHistory = await terminalHistory(mentor, retry, 'completed');
    assert.equal(retryHistory.length, 4);
    assert.equal(retryHistory[1].status, 'failed');
    assert.equal(retryHistory[3].status, 'completed');
    assert.notEqual(retryHistory[0].client_message_id, retryHistory[2].client_message_id);
    assert.deepEqual(internRequests, [], 'Mentor page fetched intern operations');

    const peer = await login('Второй наставник');
    await peer.goto(`${origin}/mentor/chat/${first}`);
    await peer.getByRole('alert').first().waitFor();
    assert.equal(await peer.getByText(firstQuestion, { exact: false }).count(), 0);
    await api(peer, `/conversations/${first}`, 404);
    await api(peer, `/conversations/${first}/messages`, 404);
    await api(peer, `/conversations/${first}`, 404, 'PATCH', { archived: true });
    await peer.getByRole('link', { name: 'Новый диалог', exact: true }).click();
    await peer.waitForURL(url => url.pathname === '/mentor/chat');
    const peerConversation = await ask(peer, 'N76 собственный вопрос второго наставника');
    assert.ok(!(await answers(peer).last().innerText()).includes('ЛИПА'));
    await api(mentor, `/conversations/${peerConversation}`, 404);

    await mentor.getByRole('button', { name: 'Выйти', exact: true }).click();
    await mentor.waitForURL(url => url.pathname === '/login');
    const intern = await login('Тестовый стажер', mentor);
    await intern.goto(`${origin}/chat?stepId=step_local_setup&direction=mentor`);
    await intern.getByText('Шаг: Запустить проект', { exact: true }).waitFor();
    assert.equal(await intern.getByText(firstQuestion, { exact: false }).count(), 0);
    const internConversation = await ask(intern, 'N76 обычный вопрос стажёра', false, false);
    assert.equal((await api(intern, `/conversations/${internConversation}`)).step_id, 'step_local_setup');
    await intern.getByRole('button', { name: 'Обратиться к специалисту' }).waitFor();
    await api(intern, `/conversations/${first}`, 404);
    await picture(intern, 'intern-regression', 360);
    await writeFile(`${directory}/state.json`, JSON.stringify({ first, retry, peer: peerConversation, intern: internConversation }));
    console.log('Mentor SSE, history, retry, archive, peer isolation and intern regression passed at 360/768/1440');
  }
  assert.deepEqual(errors, [], 'Browser page errors');
} catch (error) {
  for (const [index, context] of browser.contexts().entries()) {
    for (const page of context.pages()) {
      console.error(page.url(), (await page.locator('body').innerText()).slice(0, 2500));
      await page.screenshot({ path: `${directory}/failure-${index}.png`, fullPage: true });
    }
  }
  throw error;
} finally {
  await browser.close();
}
