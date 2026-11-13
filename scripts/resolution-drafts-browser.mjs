// Run against the normal SPA/Go API with synthetic demo accounts and the
// deterministic provider in hr-provider-fixture.mjs. No MSW interception.
import assert from 'node:assert/strict';
import { mkdir, writeFile, readFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.RESOLUTION_PLAYWRIGHT_MODULE ? pathToFileURL(process.env.RESOLUTION_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.RESOLUTION_APP_URL ?? 'http://127.0.0.1:5173';
const directory = process.env.RESOLUTION_SCREENSHOTS ?? 'tmp/resolution-browser';
await mkdir(directory, { recursive: true });
const browser = await chromium.launch({ ...(process.env.RESOLUTION_BROWSER && { executablePath: process.env.RESOLUTION_BROWSER }), headless: true });
const run = Date.now().toString(36);
const reports = [];
let hrTarget = '', mentorTarget = '';

async function login(page, name) {
  await page.goto(origin + '/login');
  await page.getByRole('radio', { name: new RegExp(name) }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => !url.pathname.endsWith('/login'));
}
async function ask(page, text) {
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(text);
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().waitFor();
}

try {
  if(process.argv.includes('--verify')) {
    const saved=JSON.parse(await readFile(`${directory}/report.json`,'utf8'));
    for(const entry of saved.filter(item=>item.width===1440)) {
      const specialist=await browser.newPage({viewport:{width:1440,height:960}});
      const intern=await browser.newPage({viewport:{width:1440,height:960}});
      await login(specialist,entry.direction==='hr'?'Анна, HR':'Тестовый наставник');
      await specialist.goto(entry.editorUrl);
      await specialist.getByRole('button',{name:'Открыть опубликованную инструкцию'}).waitFor();
      const result=await (await specialist.request.get(origin+`/api/v1/${entry.direction}/questions/${entry.question_id}/instruction-draft`)).json();
      assert.equal(result.draft.document_id,entry.document_id);assert.equal(result.draft.document_version,entry.version);assert.equal(result.draft.status,'ready');
      await login(intern,'Тестовый стажер');await intern.goto(entry.conversation);
      await intern.getByRole('button',{name:'Обратиться к специалисту'}).first().click();
      await intern.getByRole('button',{name:'Открыть инструкцию',exact:true}).last().click();await intern.getByRole('dialog',{name:result.draft.title,exact:true}).waitFor();
      await Promise.all([specialist.close(),intern.close()]);console.log(`Verified persisted ${entry.direction} publication after API restart: version ${entry.version}`);
    }
  } else for (const width of [360, 768, 1440]) {
    for (const direction of ['mentor', 'hr']) {
      const intern = await browser.newPage({ viewport: { width, height: 960 } });
      const specialist = await browser.newPage({ viewport: { width, height: 960 } });
      const foreign = await browser.newPage({ viewport: { width, height: 960 } });
      const errors = [];
      for (const page of [intern, specialist]) page.on('pageerror', error => errors.push(error.message));
      await login(intern, 'Тестовый стажер');
      await login(specialist, direction === 'hr' ? 'Анна, HR' : 'Тестовый наставник');
      await login(foreign, direction === 'hr' ? 'Тестовый наставник' : 'Денис, HR');
      const questionText = `${direction === 'hr' ? 'Как оформить отпуск' : 'Как настроить сервис'} N56 ${run} ${width}?`;
      await intern.goto(origin + '/chat');
      await ask(intern, questionText);
      const conversation = intern.url();
      await intern.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
      await intern.getByRole('combobox', { name: /Адресат/ }).selectOption(direction);
      await intern.getByLabel('Контекст для специалиста').fill('Только одобренный контекст для проверки N56');
      const response = intern.waitForResponse(r => r.request().method() === 'POST' && /\/questions$/.test(new URL(r.url()).pathname));
      await intern.getByRole('button', { name: 'Подтвердить отправку' }).focus();
      await intern.keyboard.press('Enter');
      const question = await (await response).json();
      assert.equal(question.direction, direction);
      assert.equal((await foreign.request.get(origin + `/api/v1/${direction}/questions/${question.id}/instruction-draft`)).status(), 403);

      const resolution = direction === 'hr' ? 'Подайте заявление через кадровый портал и согласуйте с руководителем.' : 'Проверьте конфигурацию сервиса, затем выполните локальный запуск.';
      let baseTitle = '';
      if (direction === 'mentor') {
        baseTitle = `Базовая инструкция N56 ${run} ${width}`;
        await specialist.goto(origin + '/mentor/documents');
        await specialist.getByLabel('Название документа').fill(baseTitle);
        await specialist.getByLabel('Файл Markdown').setInputFiles({ name: 'rules.md', mimeType: 'text/markdown', buffer: Buffer.from('# Настройка\nПроверьте конфигурацию сервиса.') });
        await specialist.getByRole('button', { name: 'Загрузить документ' }).click();
        await specialist.getByRole('listitem').filter({ hasText: baseTitle }).getByText('Готов к поиску', { exact: true }).waitFor();
      }
      await specialist.goto(origin + `/${direction}/questions`);
      await specialist.getByRole('link', { name: questionText, exact: true }).click();
      if (direction === 'hr') {
        await specialist.getByRole('button', { name: 'Взять в работу' }).click();
        await specialist.getByLabel('Ответ стажёру').fill(resolution);
        await specialist.getByRole('button', { name: 'Решить обращение' }).click();
      } else {
        await specialist.getByLabel('Комментарий наставника').fill(resolution);
        await specialist.getByRole('checkbox', { name: baseTitle }).check();
        await specialist.getByRole('button', { name: 'Подтвердить решение' }).click();
      }
      await specialist.getByRole('link', { name: /Подготовить инструкцию/ }).click();
      await specialist.getByRole('button', { name: 'Предложить текст', exact: true }).click();
      await specialist.getByText('Предложение помощника. Проверьте и отредактируйте текст.').waitFor();
      const previous = direction === 'hr' ? hrTarget : mentorTarget;
      if (previous) await specialist.getByLabel('Куда опубликовать').selectOption(previous);
      const contentField = specialist.getByRole('textbox', { name: previous ? /^Дополнение к документу/ : /^Текст инструкции/ });
      const title = `Инструкция из решения N56 ${direction} ${run} ${width}`;
      await specialist.getByLabel('Название инструкции').fill(title);
      // Verify logical keyboard order without mouse-only interaction.
      await specialist.getByLabel('Название инструкции').focus();
      await specialist.keyboard.press('Tab');
      assert.equal(await contentField.evaluate(el => el === document.activeElement), true);
      await contentField.fill(`## Проверенный порядок\n${resolution}\n\nПоставка N56 ${run} ${width}.`);
      await specialist.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
      await specialist.getByText('Все изменения сохранены', { exact: true }).waitFor();
      const editorUrl = specialist.url();
      await specialist.reload();
      assert.match(await contentField.inputValue(), new RegExp(`Поставка N56 ${run} ${width}`));
      assert.equal(await specialist.getByRole('button', { name: 'Опубликовать инструкцию', exact: true }).isEnabled(), false);
      assert.equal(await specialist.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await specialist.screenshot({ path: `${directory}/${direction}-editor-${width}.png`, fullPage: true });
      await specialist.getByRole('checkbox', { name: /Я проверил текст и аудиторию/ }).check();
      await specialist.getByRole('button', { name: 'Опубликовать инструкцию', exact: true }).focus();
      await specialist.keyboard.press('Enter');
      await specialist.getByRole('button', { name: 'Открыть опубликованную инструкцию' }).waitFor();
      const publication = await (await specialist.request.get(origin + `/api/v1/${direction}/questions/${question.id}/instruction-draft`)).json();
      const doc = publication.draft;
      assert.equal(doc.status, 'ready');
      if (previous) assert.equal(doc.document_id, previous);
      if (direction === 'hr') hrTarget = doc.document_id; else mentorTarget = doc.document_id;
      await specialist.getByRole('button', { name: 'Открыть опубликованную инструкцию' }).click();
      await specialist.getByRole('dialog', { name: title, exact: true }).waitFor();
      await specialist.keyboard.press('Escape');

      await intern.goto(conversation);
      await intern.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
      await intern.getByRole('button', { name: 'Открыть инструкцию', exact: true }).last().click();
      await intern.getByRole('dialog', { name: title, exact: true }).waitFor();
      await intern.keyboard.press('Escape');
      if (direction === 'hr') {
        await ask(intern, 'Где описан порядок оформления отпуска?');
        await intern.getByRole('list', { name: 'Источники ответа' }).last().getByText(new RegExp(`Версия ${doc.document_version}`)).waitFor();
      }
      assert.equal(await intern.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await intern.screenshot({ path: `${directory}/${direction}-intern-${width}.png`, fullPage: true });
      assert.deepEqual(errors, []);
      reports.push({ width, direction, editorUrl, conversation, question_id: question.id, document_id: doc.document_id, version: doc.document_version, noHorizontalOverflow: true, pageErrors: errors });
      await writeFile(`${directory}/report.json`, JSON.stringify(reports, null, 2));
      await Promise.all([intern.close(), specialist.close(), foreign.close()]);
      console.log(`Resolution draft scenario passed: ${direction} ${width}px version ${doc.document_version}`);
    }
  }
} catch(error) {
  for (const [i,context] of browser.contexts().entries()) for(const page of context.pages()) {
    console.error(`Browser failure context ${i}: ${page.url()}\n${await page.locator('body').innerText()}`);
    await page.screenshot({path:`${directory}/failure-${i}.png`,fullPage:true});
  }
  throw error;
} finally { await browser.close(); }
