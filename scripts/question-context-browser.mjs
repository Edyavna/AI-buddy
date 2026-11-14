// Ordinary SPA routes, real cookie sessions, Go and PostgreSQL. Only the
// external model is replaced by hr-provider-fixture.mjs; no browser API mocks.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.CONTEXT_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.CONTEXT_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.CONTEXT_APP_URL ?? 'http://localhost:5173';
const directory = process.env.CONTEXT_SCREENSHOTS ?? 'tmp/question-context-browser';
await mkdir(directory, { recursive: true });
const browser = await chromium.launch({ headless: true,
  ...(process.env.CONTEXT_BROWSER && { executablePath: process.env.CONTEXT_BROWSER }) });
const run = Date.now().toString(36), reports = [];
const approval = 'Я проверил вопрос, адресата и весь отправляемый контекст';
const quote = 'Проверенное стажёром резюме N57';
const attempts = 'Проверил конфигурацию вручную; повторил локальный запуск.';
const exact = text => new RegExp('^' + text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));

async function login(page, name) {
  await page.goto(origin + '/login');
  await page.getByRole('radio', { name: new RegExp(name) }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => !url.pathname.endsWith('/login'));
}
async function json(page, path) {
  const response = await page.request.get(origin + '/api/v1' + path);
  assert.equal(response.status(), 200, path + ': ' + await response.text());
  return response.json();
}
async function fits(page) {
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
}
async function capture(page, name, fullPage = true) {
  await page.evaluate(() => { window.scrollTo(0, 0); document.activeElement?.blur(); });
  await page.screenshot({ path: `${directory}/${name}.png`, fullPage, animations: 'disabled' });
}
async function openComposer(page) {
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
  await page.getByRole('heading', { name: 'Подтверждённые данные и выбранные фрагменты' }).waitFor();
}
async function sendQuestion(page, text) {
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(text);
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().waitFor();
}
async function checkSnapshot(page, item) {
  const snapshot = page.getByRole('region', { name: 'Отправленный снимок контекста' }).last();
  await snapshot.getByText(quote, { exact: true }).waitFor();
  assert.match(await snapshot.innerText(), /со слов стажёра/);
  assert.match(await snapshot.innerText(), /Открыт стажёром перед отправкой/);
  assert.match(await snapshot.innerText(), /Отредактировано и подтверждено/);
  assert.equal(await snapshot.getByRole('link', { name: /Сообщение/ }).count(), 1);
  assert.equal(await snapshot.getByRole('link', { name: /Сообщение/ }).first().getAttribute('href'), `#context-message-${item.fragment_id}`);
  await snapshot.getByRole('button', { name: 'Открыть источник контекста' }).click();
  await page.getByRole('dialog', { name: item.document_title, exact: true }).waitFor();
  await fits(page);
  await page.keyboard.press('Escape');
}
async function prepareDocuments() {
  const actor = await browser.newPage();
  await login(actor, 'Тестовый наставник');
  await actor.goto(origin + '/mentor/documents');
  await actor.getByLabel('Название документа').fill('Настройка сервиса N57 ' + run);
  await actor.getByLabel('Файл Markdown').setInputFiles({ name: 'context-rules.md', mimeType: 'text/markdown',
    buffer: Buffer.from('# Настройка сервиса\nПроверьте конфигурацию перед локальным запуском.') });
  await actor.getByRole('button', { name: 'Загрузить документ' }).click();
  await actor.getByRole('listitem').filter({ hasText: 'Настройка сервиса N57 ' + run }).getByText('Готов к поиску', { exact: true }).waitFor();
  await actor.close();
  const hr = await browser.newPage();
  await login(hr, 'Анна, HR');
  await hr.goto(origin + '/hr/documents');
  await hr.getByRole('button', { name: 'Новая инструкция' }).click();
  await hr.getByLabel('Название', { exact: true }).fill('Оформление отпуска N57 ' + run);
  await hr.getByLabel('Текст Markdown').fill('# Отпуск\nПодайте заявление через кадровый портал.');
  await hr.getByRole('checkbox', { name: /backend-команда/ }).check();
  await hr.getByRole('button', { name: 'Сохранить черновик' }).click();
  await hr.getByRole('button', { name: 'Опубликовать', exact: true }).click();
  await hr.getByRole('listitem').filter({ hasText: 'Оформление отпуска N57 ' + run }).getByText('Опубликован · версия 1', { exact: true }).waitFor();
  await hr.close();
}

try {
  if (process.argv.includes('--verify')) {
    const saved = JSON.parse(await readFile(`${directory}/report.json`, 'utf8'));
    for (const item of saved.filter(row => row.width === 1440)) {
      const intern = await browser.newPage(), actor = await browser.newPage();
      await login(intern, 'Тестовый стажер');
      await intern.goto(item.conversation);
      await openComposer(intern);
      await intern.getByRole('region', { name: 'Отправленный снимок контекста' }).getByText(quote, { exact: true }).waitFor();
      await login(actor, item.direction === 'hr' ? 'Анна, HR' : 'Тестовый наставник');
      await actor.goto(item.question_url);
      await checkSnapshot(actor, item);
      const data = await json(actor, `/${item.direction}/questions/${item.id}`);
      assert.equal(data.context_snapshot.attempts, attempts);
      assert.equal(data.context_snapshot.documents[0].opened, true);
      assert.equal(data.context_snapshot.fragments.length, 1);
      await Promise.all([intern.close(), actor.close()]);
      console.log(`Restored ${item.direction} context after API restart/repeated seed`);
    }
  } else {
    await prepareDocuments();
    for (const width of [360, 768, 1440]) for (const direction of ['mentor', 'hr']) {
      const internContext = await browser.newContext({ viewport: { width, height: 960 } });
      const intern = await internContext.newPage();
      const actor = await browser.newPage({ viewport: { width, height: 960 } });
      const errors = [];
      for (const page of [intern, actor]) page.on('pageerror', error => errors.push(error.message));
      await login(intern, 'Тестовый стажер');
      await login(actor, direction === 'hr' ? 'Анна, HR' : 'Тестовый наставник');
      // Both a checkpoint-bound conversation and an ordinary unbound chat.
      if (width === 768) await intern.goto(origin + '/chat');
      else {
        await intern.goto(origin + '/onboarding');
        await intern.getByRole('link', { name: /^Спросить помощника о шаге:/ }).first().click();
      }
      await sendQuestion(intern, `${direction === 'hr' ? 'Как оформить отпуск' : 'Как настроить сервис'} N57 ${run} ${width}?`);
      const conversation = intern.url();
      await openComposer(intern);
      await intern.getByText(new RegExp('Предложенный адресат — ' + (direction === 'hr' ? 'HR' : 'Наставник'))).waitFor();
      const conversationId = new URL(conversation).pathname.split('/').at(-1);
      const messages = await json(intern, `/conversations/${conversationId}/messages`);
      const message = messages.filter(m => m.role === 'assistant').at(-1);
      assert.equal(message.status, 'completed');
      const preview = await json(intern, `/messages/${message.id}/question-context?direction=${direction}`);
      assert.ok(preview.fragments.length >= 2);
      assert.ok(preview.documents.length, 'The provider must actually search and read a permitted source');
      assert.equal(preview.documents[0].opened, false);
      assert.equal(preview.step === null, width === 768);
      const document = preview.documents[0];
      const sourceButton = intern.getByRole('button', { name: exact('Открыть источник: ' + document.title) });
      const acknowledgement = intern.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/opened') && r.status() === 200);
      await sourceButton.click();
      const dialog = intern.getByRole('dialog', { name: document.title, exact: true });
      await dialog.waitFor();
      await acknowledgement;
      await dialog.getByRole('button', { name: /Закрыть/ }).first().focus();
      await intern.keyboard.press('Shift+Tab');
      assert.equal(await dialog.evaluate(el => el.contains(document.activeElement)), true);
      await capture(intern, `${direction}-document-${width}`, false);
      await intern.keyboard.press('Escape');
      assert.equal(await sourceButton.evaluate(el => el === document.activeElement), true);
      await intern.getByRole('checkbox', { name: /Открыта вами/ }).first().waitFor();
      await intern.getByRole('checkbox', { name: /Включить сообщение 1:/ }).check();
      await intern.getByRole('checkbox', { name: /Включить сообщение 2:/ }).check();
      await intern.getByRole('checkbox', { name: /Включить сообщение 2:/ }).uncheck();
      await intern.getByRole('textbox', { name: 'Резюме сообщения 1', exact: true }).fill(quote);
      await intern.getByRole('checkbox', { name: new RegExp('^' + document.title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '.*Открыта вами') }).check();
      if (width !== 768) await intern.getByRole('checkbox', { name: /Включить чекпоинт:/ }).check();
      await intern.getByLabel('Контекст для специалиста').fill('Одобренное краткое резюме N57');
      await intern.getByLabel('Что уже пробовал — со слов стажёра').fill(attempts);
      const title = `${direction === 'hr' ? 'Уточнить оформление отпуска' : 'Уточнить настройку сервиса'} N57 ${run} ${width}`;
      await intern.getByRole('textbox', { name: 'Вопрос', exact: true }).fill(title);
      if (width === 1440) {
        const popup = intern.waitForEvent('popup');
        await intern.getByRole('link', { name: 'Исходное сообщение 1', exact: true }).click();
        const source = await popup;
        await source.waitForURL(url => url.hash === '#message-' + preview.fragments[0].message_id);
        await source.locator('[id="message-' + preview.fragments[0].message_id + '"]').waitFor();
        await source.close();
      }
      const saving = intern.waitForResponse(r => r.request().method() === 'PUT' && r.url().endsWith('/question-drafts/' + direction));
      await intern.getByRole('button', { name: 'Сохранить черновик', exact: true }).focus();
      await intern.keyboard.press('Enter');
      assert.equal((await saving).status(), 200);
      await intern.reload();
      await openComposer(intern);
      await intern.getByRole('textbox', { name: 'Резюме сообщения 1', exact: true }).waitFor();
      assert.equal(await intern.getByRole('textbox', { name: 'Резюме сообщения 1', exact: true }).inputValue(), quote);
      assert.equal(await intern.getByRole('checkbox', { name: approval }).isChecked(), false);
      await fits(intern);
      await capture(intern, `${direction}-preview-${width}`);
      await intern.getByRole('checkbox', { name: approval }).focus();
      await intern.keyboard.press('Space');
      const posting = intern.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/messages/' + message.id + '/questions'));
      await intern.getByRole('button', { name: 'Подтвердить отправку', exact: true }).focus();
      await intern.keyboard.press('Enter');
      const response = await posting;
      assert.equal(response.status(), 200, await response.text());
      const result = await response.json();
      assert.equal(result.context_snapshot.fragments.length, 1);
      assert.equal(result.context_snapshot.fragments[0].text, quote);
      assert.equal(result.context_snapshot.fragments[0].edited, true);
      assert.equal(result.context_snapshot.documents[0].opened, true);
      assert.equal(result.context_snapshot.step === null, width === 768);
      const item = { width, direction, id: result.id, conversation, title,
        question_url: `${origin}/${direction}/questions/${result.id}`,
        fragment_id: result.context_snapshot.fragments[0].message_id,
        document_id: document.document_id, document_title: document.title };
      await actor.goto(item.question_url);
      await checkSnapshot(actor, item);
      await fits(actor);
      await capture(actor, `${direction}-recipient-${width}`);
      // Specialist only receives the chosen projection, never the entire chat.
      assert.equal((await actor.request.get(origin + `/api/v1/conversations/${conversationId}/messages`)).status(), 403);
      assert.equal((await intern.request.get(origin + `/api/v1/${direction}/questions/${result.id}/context-documents/${document.document_id}`)).status(), 403);
      assert.deepEqual(errors, []);
      reports.push(item);
      await writeFile(`${directory}/report.json`, JSON.stringify(reports, null, 2));
      await Promise.all([internContext.close(), actor.close()]);
      console.log(`Context passed ${direction} ${width}px: opened source, removed fragment, exact approval, durable draft, permitted recipient`);
    }
  }
} catch (error) {
  for (const [i, context] of browser.contexts().entries()) for (const page of context.pages()) {
    console.error(`Failure ${i}: ${page.url()}\n${await page.locator('body').innerText()}`);
    await page.screenshot({ path: `${directory}/failure-${i}.png`, fullPage: true });
  }
  throw error;
} finally { await browser.close(); }
