import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { chromium } from 'playwright-core';

const origin = process.env.MEETING_APP_URL || 'http://localhost:5173';
const browser = await chromium.launch({ executablePath: process.env.MEETING_BROWSER_EXECUTABLE });
const date = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
const subject = 'Цели на месяц ' + Date.now();

async function login(name, width) {
  const page = await browser.newPage({ viewport: { width, height: 900 }, timezoneId: 'UTC' });
  await page.goto(origin + '/login');
  await page.getByRole('radio', { name }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');
  return page;
}

async function change(page, button, action) {
  const result = page.waitForResponse(response => {
    const request = response.request();
    return request.method() === 'POST' && new URL(response.url()).pathname === '/api/v1/meetings' &&
      request.postDataJSON().action === action;
  });
  await button.click();
  const response = await result;
  assert.equal(response.status(), 200, await response.text());
}

try {
  const intern = await login(/Тестовый стажер/, 360);
  const mentor = await login(/Тестовый наставник/, 768);
  const foreign = await login(/Стажер другой команды/, 1440);
  await intern.goto(origin + '/chat');
  await intern.getByLabel('Ваш вопрос').fill(
    `Создай встречу с моим наставником ${date} в 15:00 по Москве. Тема — ${subject}. В повестку добавь первую задачу и критерии успеха`,
  );
  await intern.getByRole('button', { name: 'Отправить', exact: true }).click();
  await intern.getByRole('link', { name: 'Открыть черновик встречи' }).click();
  assert.equal(await intern.getByLabel('Тема встречи', { exact: true }).inputValue(), subject);
  assert.equal(await intern.getByLabel('Время', { exact: true }).inputValue(), '15:00');
  assert.equal(await intern.getByLabel('Часовой пояс', { exact: true }).inputValue(), 'Europe/Moscow');
  await intern.getByLabel(/Свои темы/).fill('Первая задача\nКритерии успеха\nМоя тема');
  await intern.getByLabel('Ссылка на звонок').fill('https://example.com/call');
  const sizes = await intern.evaluate(() => [document.documentElement.scrollWidth, innerWidth]);
  assert.ok(sizes[0] <= sizes[1], `mobile layout overflow ${sizes}`);
  await change(intern, intern.getByRole('button', { name: 'Отправить приглашение' }), 'invite');
  const card = page => page.getByRole('article').filter({ has: page.getByRole('heading', { name: new RegExp(subject) }) });
  await card(intern).getByText(/Ожидает ответа наставника/).waitFor();
  const meetingID = (await card(intern).getAttribute('id')).slice('meeting-'.length);
  assert.equal(await card(intern).count(), 1);

  await mentor.goto(origin + '/mentor/meetings');
  await card(mentor).getByText(/Моя тема/).waitFor();
  await change(mentor, card(mentor).getByRole('button', { name: 'Принять приглашение' }), 'accept');
  await card(mentor).getByText(/^Принята/).waitFor();

  await intern.reload();
  await card(intern).getByText(/^Принята/).waitFor();
  const start = intern.waitForEvent('download');
  await card(intern).getByRole('link', { name: 'Скачать .ics' }).click();
  const download = await start;
  assert.equal(download.suggestedFilename(), 'meeting.ics');
  const calendar = (await readFile(await download.path(), 'utf8')).replaceAll('\r\n ', '');
  assert.match(calendar, new RegExp('DTSTART:' + date.replaceAll('-', '') + 'T120000Z'));
  assert.ok(calendar.includes(`${origin}/meetings#meeting-${meetingID}`));
  assert.ok(calendar.includes('Europe/Moscow'));
  assert.equal((await foreign.request.get(`${origin}/api/v1/meetings/${meetingID}/calendar`)).status(), 404);

  await card(intern).getByLabel(/Новая дата и время/).fill(`${date}T13:00`);
  await change(intern, card(intern).getByRole('button', { name: 'Перенести встречу' }), 'reschedule');
  await card(intern).getByText(/Ожидает ответа наставника/).waitFor();
  assert.equal(await card(intern).getByRole('link', { name: 'Скачать .ics' }).count(), 0);
  await mentor.reload();
  await change(mentor, card(mentor).getByRole('button', { name: 'Отклонить приглашение' }), 'decline');
  await intern.reload();
  await card(intern).getByText(/^Отклонена/).waitFor();
  console.log('PASS chat draft, invitation, acceptance, calendar, access, reschedule and decline over Go/PostgreSQL');
} finally {
  await browser.close();
}
