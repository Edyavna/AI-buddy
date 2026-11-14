import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.QUEUE_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.QUEUE_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.QUEUE_APP_URL ?? 'http://localhost:5173';
const directory = 'tmp/action-queue-browser';
const note = 'Вручную добавлен в командный канал';
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.QUEUE_BROWSER,
});
await mkdir(directory, { recursive: true });

async function login(page, name) {
  await page.goto(`${origin}/login`);
  await page.getByRole('radio', { name }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');
}

async function teamStep(page) {
  const open = page.getByRole('button', { name: /Организационные моменты/ });
  await open.click();
  return page.getByRole('region', { name: 'Приглашение в закрытые чаты' });
}

async function ask(page, direction, text) {
  await page.goto(`${origin}/chat`);
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill(text);
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
  await page.getByRole('combobox', { name: /Адресат/ }).selectOption(direction);
  await page.getByLabel('Контекст для специалиста').fill('Проверка очереди');
  await page.getByRole('checkbox', { name: 'Я проверил вопрос, адресата и весь отправляемый контекст' }).check();
  const created = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/questions'));
  await page.getByRole('button', { name: 'Подтвердить отправку' }).click();
  const response = await created;
  assert.equal(response.status(), 200);
  return (await response.json()).id;
}

function action(page, name, href) {
  const link = page.getByRole('link', { name, exact: true });
  return href ? link.and(page.locator(`a[href="${href}"]`)) : link;
}

async function responsive(page, label, width) {
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${label} at ${width}px`);
  await page.screenshot({ path: `${directory}/${label}-${width}.png`, fullPage: true });
}

try {
  if (process.argv.includes('--verify')) {
    const intern = await browser.newPage();
    await login(intern, 'Тестовый стажер');
    const step = await teamStep(intern);
    await step.getByText(`Наставник отметил, что пригласил вас вручную. ${note}`).waitFor();
    const mentor = await browser.newPage();
    await login(mentor, 'Тестовый наставник');
    await action(mentor, 'Мои действия').click();
    await mentor.getByLabel('Тип действия').selectOption('invitation');
    await mentor.getByText('Нет ожидающих действий').waitFor();
    console.log('Manual completion survived API restart and repeated seed');
  } else {
    const intern = await browser.newPage({ viewport: { width: 360, height: 950 } });
    const mentor = await browser.newPage({ viewport: { width: 360, height: 950 } });
    const hr = await browser.newPage({ viewport: { width: 360, height: 950 } });
    await login(intern, 'Тестовый стажер');
    await login(mentor, 'Тестовый наставник');
    await login(hr, 'Анна, HR');
    const step = await teamStep(intern);
    const existing = step.getByRole('button', { name: 'Отменить запрос' });
    await step.getByRole('button').first().waitFor();
    if (await existing.isVisible()) await existing.click();
    await step.getByRole('button', { name: /Попросить приглашение|Запросить ещё одно приглашение/ }).click();
    await step.getByText('Наставник получил запрос на ручное приглашение.').waitFor();
    await step.getByRole('button', { name: 'Отменить запрос' }).click();
    await step.getByRole('button', { name: 'Попросить приглашение' }).click();
    await step.getByText('Наставник получил запрос на ручное приглашение.').waitFor();
    await responsive(intern, 'intern-invitation', 360);
    const checkpoint = intern.locator('article[data-step-id]').filter({ has: step });
    const revoke = checkpoint.getByRole('button', { name: 'Отозвать подтверждение' });
    if (await revoke.isVisible()) await revoke.click();
    await checkpoint.getByRole('button', { name: 'Подтвердить выполнение' }).click();
    await checkpoint.getByText('Ожидаем наставника').waitFor();

    const hrID = await ask(intern, 'hr', 'Как оформить ежегодный отпуск?');
    const workID = await ask(intern, 'mentor', 'Как настроить проект команды?');
    await Promise.all([action(mentor, 'Мои действия').click(), action(hr, 'Мои действия').click()]);
    const hrQuestion = action(hr, 'Взять обращение', `/hr/questions/${hrID}`);
    await hrQuestion.waitFor();
    await action(mentor, 'Открыть вопрос', `/mentor/questions/${workID}`).waitFor();
    for (const width of [360, 768, 1440]) {
      for (const page of [mentor, hr]) await page.setViewportSize({ width, height: 950 });
      await responsive(mentor, 'mentor', width);
      await responsive(hr, 'hr', width);
    }
    const invite = mentor.getByRole('listitem').filter({ hasText: 'Стажёр ждёт приглашения в закрытые чаты' });
    const confirm = mentor.getByRole('listitem').filter({ hasText: 'Организационные моменты' })
      .filter({ hasText: 'Чекпоинт ожидает подтверждения наставника' });
    await action(confirm, 'К чекпоинту').focus();
    await mentor.keyboard.press('Enter');
    const mentorStep = mentor.getByRole('listitem').filter({ has: mentor.getByRole('heading', { name: 'Организационные моменты' }) });
    await mentorStep.getByRole('button', { name: 'Подтвердить свою часть' }).waitFor();
    assert.equal(await mentorStep.evaluate(element => document.activeElement === element), true);
    await mentorStep.getByRole('button', { name: 'Подтвердить свою часть' }).click();
    await mentorStep.getByRole('button', { name: 'Отозвать свою отметку' }).waitFor();
    await action(mentor, 'Мои действия').click();
    await invite.getByRole('textbox', { name: /Пояснение к приглашению/ }).fill(note);
    await invite.getByRole('button', { name: 'Отметить ручное приглашение' }).focus();
    await mentor.keyboard.press('Enter');
    await mentor.getByText('Ручное приглашение отмечено.').waitFor();
    assert.equal(await mentor.getByRole('heading', { level: 1 }).evaluate(element => document.activeElement === element), true);
    await mentor.reload();
    await mentor.getByLabel('Тип действия').selectOption('invitation');
    await mentor.getByText('Нет ожидающих действий').waitFor();

    await hrQuestion.click();
    await hr.getByRole('button', { name: 'Взять в работу' }).click();
    await hr.getByLabel('Ответ стажёру').fill('Отправьте заявление через кадровый портал.');
    await hr.getByRole('button', { name: 'Решить обращение' }).click();
    await hr.getByRole('button', { name: /Вернуть в работу|Переоткрыть/ }).waitFor();
    await action(hr, 'Мои действия').click();
    await hr.locator(`a[href="/hr/questions/${hrID}"]`).waitFor({ state: 'hidden' });
    console.log('Mentor and HR queue passed at 360, 768 and 1440 px');
  }
} catch (error) {
  for (const [index, context] of browser.contexts().entries()) {
    for (const page of context.pages()) {
      console.error(page.url(), (await page.locator('body').innerText()).slice(0, 3500));
      await page.screenshot({ path: `${directory}/failure-${index}.png`, fullPage: true });
    }
  }
  throw error;
} finally {
  await browser.close();
}
