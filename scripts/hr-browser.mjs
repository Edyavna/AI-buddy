import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.HR_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.HR_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.HR_APP_URL ?? 'http://localhost:5173';
const screenshotDir = process.env.HR_SCREENSHOTS ?? 'tmp/hr-browser';
await mkdir(screenshotDir, { recursive: true });
const browser = await chromium.launch({ executablePath: process.env.HR_BROWSER, headless: true });
const question = 'Как оформить отпуск?';
const run = Date.now().toString(36);
const title = 'Правила отпуска ' + run;
const resolution = 'Подайте заявление через кадровый портал.';

async function login(page, name) {
  await page.goto(origin + '/login');
  await page.getByRole('radio', { name: new RegExp(name) }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
}

async function ask(page, text) {
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(text);
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
}

try {
  for (const width of [360, 768, 1440]) {
    const intern = await browser.newPage({ viewport: { width, height: 900 } });
    const hr = await browser.newPage({ viewport: { width, height: 900 } });
    const mentor = await browser.newPage({ viewport: { width, height: 900 } });
    await login(intern, 'Тестовый стажер');
    await login(hr, 'Анна, HR');
    await login(mentor, 'Тестовый наставник');
    await mentor.getByRole('heading', { name: 'Вопросы', exact: false }).first().waitFor();

    await intern.getByRole('link', { name: 'Чат', exact: true }).click();
    await ask(intern, question + ' ' + run + ' ' + width);
    await intern.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
    await intern.getByText(/Предложенный адресат — HR/).waitFor();
    await intern.getByLabel('Контекст для специалиста').fill('Контекст, одобренный стажёром');
    await intern.getByRole('checkbox', { name: 'Я проверил вопрос, адресата и весь отправляемый контекст' }).check();
    const confirm = intern.getByRole('button', { name: 'Подтвердить отправку' });
    await confirm.focus();
    await intern.keyboard.press('Enter');
    await intern.getByText('HR · Новое').waitFor();
    const conversation = intern.url();

    await hr.getByRole('link', { name: 'Инструкции' }).click();
    await hr.getByRole('button', { name: 'Новая инструкция' }).click();
    await hr.getByLabel('Название').fill(title + ' ' + width);
    await hr.getByLabel('Текст Markdown').fill('# Отпуск\n' + resolution);
    await hr.getByRole('checkbox', { name: /backend-команда/ }).check();
    await hr.getByRole('button', { name: 'Сохранить черновик' }).click();
    await hr.getByRole('button', { name: 'Опубликовать' }).click();
    await hr.getByRole('listitem').filter({ hasText: title + ' ' + width }).getByText('Опубликован · версия 1', { exact: true }).waitFor();

    await hr.getByRole('link', { name: 'Обращения', exact: true }).click();
    await hr.getByRole('link', { name: question + ' ' + run + ' ' + width, exact: true }).click();
    await hr.getByRole('button', { name: 'Взять в работу' }).click();
    await hr.getByRole('textbox', { name: 'Ответ стажёру' }).fill(resolution);
    await hr.getByRole('checkbox', { name: title + ' ' + width }).check();
    await hr.getByRole('button', { name: 'Решить обращение' }).click();
    await hr.getByRole('button', { name: 'Вернуть в работу' }).waitFor();
    await hr.screenshot({ path: `${screenshotDir}/hr-${width}.png`, fullPage: true });
    const itemUrl = new URL(hr.url());
    const privateQuestion = itemUrl.pathname.split('/').at(-1);
    const hidden = await mentor.request.get(origin + '/api/v1/mentor/questions/' + privateQuestion);
    assert.equal(hidden.status(), 404);

    if (width === 1440 && process.env.HR_RESTART_COMPOSE) {
      execFileSync('docker', ['compose', '-f', 'compose.yaml', '-f', 'scripts/compose.hr-test.yaml', 'restart', 'api', 'hr-fixture'], { stdio: 'inherit' });
      execFileSync('docker', ['compose', '-f', 'compose.yaml', '-f', 'scripts/compose.hr-test.yaml', '--profile', 'demo', 'run', '--rm', 'seed'], { stdio: 'inherit' });
    }

    if (width === 1440 && process.env.HR_RESTART_SCRIPT) {
      execFileSync(process.env.HR_RESTART_SCRIPT, { shell: true, stdio: 'inherit' });
    }
    await intern.goto(conversation);
    await intern.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
    await intern.getByText('HR · Решено').waitFor();
    await intern.getByRole('button', { name: 'Открыть инструкцию' }).click();
    await intern.getByRole('dialog', { name: title + ' ' + width }).waitFor();
    await intern.keyboard.press('Escape');
    await ask(intern, 'Где описан порядок оформления отпуска?');
    await intern.getByRole('list', { name: 'Источники ответа' }).last().getByText(/Версия 1/).waitFor();
    assert.equal(await hr.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    assert.equal(await intern.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await intern.screenshot({ path: `${screenshotDir}/intern-${width}.png`, fullPage: true });
    await Promise.all([intern.close(), hr.close(), mentor.close()]);
    console.log(`HR browser scenario passed at ${width}px`);
  }
} finally {
  await browser.close();
}
