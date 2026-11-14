import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { chromium } from 'playwright-core';

const origin = 'http://localhost:5173';
const directory = 'tmp/onboarding-operations-browser';
const browser = await chromium.launch();
await mkdir(directory, { recursive: true });

async function login(page, name) {
  await page.goto(origin + '/login');
  await page.getByRole('radio', { name }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');
}
async function action(page, route, button) {
  await button.and(page.locator(':enabled')).waitFor();
  const result = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/' + route);
  await button.focus();
  await page.keyboard.press('Enter');
  const response = await result;
  assert.equal(response.status(), 200, await response.text());
}
async function screenshot(page, name) {
  await page.screenshot({ path: `${directory}/${name}.png`, fullPage: true });
  const dimensions = await page.evaluate(() => ({ page: document.documentElement.scrollWidth, viewport: innerWidth }));
  assert.ok(dimensions.page <= dimensions.viewport, `${name}: page ${dimensions.page}px, viewport ${dimensions.viewport}px`);
}

try {
  if (process.argv.includes('--verify')) {
    const report = JSON.parse(await readFile(`${directory}/report.json`, 'utf8'));
    const intern = await browser.newPage();
    const mentor = await browser.newPage();
    await login(intern, /Тестовый стажер/);
    await login(mentor, /Тестовый наставник/);
    await intern.goto(origin + '/meetings');
    await intern.getByText(report.at(-1).agreement, { exact: false }).waitFor();
    await mentor.goto(origin + '/curator');
    await mentor.getByLabel('Команда', { exact: true }).selectOption('team_demo_backend');
    await mentor.getByRole('heading', { name: report.at(-1).problem, exact: true }).waitFor();
  } else {
    const report = [];
    for (const width of [360, 768, 1440]) {
      const intern = await browser.newPage({ viewport: { width, height: 900 } });
      const mentor = await browser.newPage({ viewport: { width, height: 900 } });
      await login(intern, /Тестовый стажер/);
      await login(mentor, /Тестовый наставник/);
      await intern.goto(origin + '/meetings');
      await intern.getByLabel(/^Дата и время/).fill('2030-05-01T09:00');
      await action(intern, 'meetings', intern.getByRole('button', { name: 'Запланировать встречу' }));
      const card = intern.getByRole('article').filter({ has: intern.getByRole('button', { name: 'Завершить встречу' }) }).first();
      const secret = 'Личная заметка ' + width;
      const agreement = 'Договорённость ' + width;
      await card.getByLabel('Тема или личная заметка').fill(secret);
      await card.getByLabel('Видна только мне').check();
      await action(intern, 'meetings', card.getByRole('button', { name: 'Сохранить тему' }));
      await mentor.goto(origin + '/mentor/meetings');
      await mentor.getByRole('article').first().waitFor();
      assert.equal(await mentor.getByText(secret, { exact: false }).count(), 0);
      await screenshot(intern, 'meetings-' + width);
      await card.getByLabel('Итоги встречи').fill('Итоги ' + width);
      await card.getByRole('button', { name: 'Добавить договорённость' }).click();
      await card.getByLabel('Текст', { exact: true }).fill(agreement);
      await action(intern, 'meetings', card.getByRole('button', { name: 'Завершить встречу' }));
      await intern.goto(origin + '/onboarding');
      await intern.getByRole('button', { name: 'Показать договорённости' }).click();
      await intern.getByText(agreement, { exact: false }).waitFor();
      await screenshot(intern, 'agreements-' + width);

      await mentor.goto(origin + '/curator');
      await mentor.getByLabel('Команда', { exact: true }).selectOption('team_demo_backend');
      await mentor.getByRole('button', { name: 'Добавить проблему' }).click();
      const problem = 'Задержка доступа ' + width;
      await mentor.getByLabel('Название', { exact: true }).first().fill(problem);
      await mentor.getByLabel('Описание', { exact: true }).first().fill('Проверить очереди доступа');
      await action(mentor, 'curator', mentor.getByRole('button', { name: 'Сохранить проблему' }));
      await mentor.getByRole('heading', { name: problem, exact: true }).waitFor();
      await screenshot(mentor, 'curator-' + width);
      report.push({ width, agreement, problem });
      await intern.close();
      await mentor.close();
    }
    await writeFile(`${directory}/report.json`, JSON.stringify(report, null, 2));
  }
} finally {
  await browser.close();
}
