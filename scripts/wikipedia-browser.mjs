import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.WIKI_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.WIKI_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.WIKI_APP_URL ?? 'http://localhost:5173';
const browser = await chromium.launch({ executablePath: process.env.WIKI_BROWSER });

try {
  const page = await browser.newPage({ viewport: { width: 360, height: 950 } });
  await page.goto(`${origin}/login`);
  await page.getByRole('radio', { name: 'Тестовый стажер' }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');

  async function ask(question) {
    await page.goto(`${origin}/chat`);
    await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(question);
    await page.getByRole('button', { name: 'Отправить', exact: true }).click();
    await page.waitForURL(url => url.pathname.startsWith('/chat/'));
  }

  await ask('Кто ты?');
  await page.getByText(/Я AI Buddy, помощник по адаптации стажёров в Авито/).waitFor();
  await ask('Когда основали Авито?');
  await page.getByText(/Авито основали в 2007 году/).waitFor();
  await page.reload();
  await page.getByRole('button', { name: /Авито — Википедия/ }).click();
  const drawer = page.getByRole('dialog', { name: 'Авито — Википедия' });
  await drawer.getByRole('heading', { name: 'История', exact: true }).waitFor();
  const link = drawer.getByRole('link', { name: 'Открыть редакцию в Википедии' });
  const url = new URL(await link.getAttribute('href'));
  assert.equal(url.hostname, 'ru.wikipedia.org');
  assert.equal(url.protocol, 'https:');
  assert.equal(url.searchParams.get('title'), 'Авито');
  assert.equal(url.searchParams.get('oldid'), '155526969');
  assert.equal(await link.getAttribute('target'), '_blank');
  console.log('Role, persisted history, source drawer and the pinned Wikipedia link passed in Chromium.');
} finally {
  await browser.close();
}
