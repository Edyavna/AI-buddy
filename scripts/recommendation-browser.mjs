import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const { chromium } = await import(process.env.RECOMMENDATION_PLAYWRIGHT_MODULE
  ? pathToFileURL(process.env.RECOMMENDATION_PLAYWRIGHT_MODULE).href : 'playwright-core');
const origin = process.env.RECOMMENDATION_APP_URL ?? 'http://localhost:5173';
const directory = 'tmp/recommendation-browser';
await mkdir(directory, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.RECOMMENDATION_BROWSER });
const errors = [];
async function login(name) {
  const page = await browser.newPage({ viewport: { width: 360, height: 950 } });
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(`${origin}/login`);
  await page.getByRole('radio', { name }).click();
  await page.getByRole('button', { name: /Войти как/ }).click();
  await page.waitForURL(url => url.pathname !== '/login');
  return page;
}
async function api(page, path, method = 'GET', data) {
  const response = await page.request.fetch(`${origin}/api/v1${path}`, { method, data, headers: { Origin: origin } });
  assert.equal(response.status(), 200, `${method} ${path}: ${await response.text()}`);
  return response.json();
}
const recommendation = page => page.getByRole('complementary', { name: 'Рекомендованный следующий шаг' });
async function picture(page, label, width) {
  await page.setViewportSize({ width, height: 950 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `${label} overflow at ${width}`);
  await page.screenshot({ path: `${directory}/${label}-${width}.png`, fullPage: true, animations: 'disabled' });
}
async function askForHelp(page, direction, step) {
  const url = new URL(page.url());
  assert.equal(url.searchParams.get('stepId'), step);
  assert.equal(url.searchParams.get('direction'), direction);
  await page.getByRole('textbox', { name: 'Ваш вопрос' }).fill('Как настроить проект команды?');
  await page.getByRole('button', { name: 'Отправить', exact: true }).click();
  await page.waitForURL(url => url.pathname.startsWith('/chat/'));
  const conversation = new URL(page.url()).pathname.split('/').at(-1);
  assert.equal(new URL(page.url()).searchParams.get('direction'), direction);
  assert.equal((await api(page, `/conversations/${conversation}`)).step_id, step);
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
  const recipient = page.getByRole('combobox', { name: /Адресат/ }).last();
  await recipient.waitFor();
  assert.equal(await recipient.inputValue(), direction);
  await page.reload();
  await page.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
  await recipient.waitFor();
  assert.equal(await recipient.inputValue(), direction);
  return { recipient, conversation };
}

try {
  if (process.argv.includes('--verify')) {
    const state = JSON.parse(await readFile(`${directory}/state.json`, 'utf8'));
    const page = await login('Тестовый стажер');
    const plan = await api(page, '/onboarding/plan');
    assert.equal(plan.next_step_id, 'rec55-dependent');
    assert.equal(plan.recommendation.step_id, plan.next_step_id);
    assert.equal(plan.steps.find(step => step.id === 'rec55-first').status, 'completed');
    assert.equal(plan.steps.find(step => step.id === 'rec55-independent').status, 'completed');
    const hr = await login('Второй стажер');
    const question = await api(hr, `/questions/${state.question}`);
    assert.equal(question.question.direction, 'hr');
    assert.equal(question.question.step_id, 'rec55-hr');
    await hr.goto(`${origin}/chat/${state.conversation}?direction=hr`);
    await hr.getByRole('button', { name: 'Обратиться к специалисту' }).last().click();
    await hr.getByText('HR · Новое', { exact: false }).waitFor();
    console.log('Recommendation, confirmations and HR source context survived restart/reseed');
  } else {
    const page = await login('Тестовый стажер');
    const card = recommendation(page);
    await card.getByText('«Проверка результата наставником»', { exact: true }).waitFor();
    await card.getByText('В ожидании:', { exact: false }).waitFor();
    for (const width of [360, 768, 1440]) await picture(page, 'independent', width);
    const firstPlan = await api(page, '/onboarding/plan');
    assert.equal(firstPlan.next_step_id, 'rec55-independent');
    assert.equal(firstPlan.recommendation.waiting_step.state, 'waiting_mentor');
    const open = card.getByRole('button', { name: 'Рекомендация: Читать инструкцию' });
    await open.focus();
    await page.keyboard.press('Enter');
    await page.locator('article[data-step-id="rec55-independent"]').waitFor();
    await page.waitForFunction(() => document.activeElement?.id === 'current-step');
    assert.equal(await page.locator('#current-step').evaluate(element => element === document.activeElement), true);
    assert.equal(new URL(page.url()).searchParams.get('stepId'), 'rec55-independent');
    await page.reload();
    await page.locator('article[data-step-id="rec55-independent"]').waitFor();
    await page.getByRole('button', { name: 'Подтвердить выполнение', exact: true }).click();
    await card.getByRole('link', { name: 'Задать вопрос наставнику' }).waitFor();
    await card.getByRole('link', { name: 'Задать вопрос наставнику' }).click();
    await askForHelp(page, 'mentor', 'rec55-first');

    const mentor = await login('Тестовый наставник');
    const plan = await api(mentor, '/mentor/interns/intern_demo_1/plan');
    const step = plan.steps.find(step => step.id === 'rec55-first');
    await api(mentor, '/mentor/interns/intern_demo_1/steps/rec55-first/confirmation', 'PUT', { confirmed: true, revision: step.revision });
    await page.goto(`${origin}/onboarding`);
    await recommendation(page).getByText('Шаг: «Зависимый шаг»').waitFor();
    // The open page must observe mentor revocation and re-confirmation by polling.
    const confirmedPlan = await api(mentor, '/mentor/interns/intern_demo_1/plan');
    await api(mentor, '/mentor/interns/intern_demo_1/steps/rec55-first/confirmation', 'PUT', {
      confirmed: false, revision: confirmedPlan.steps.find(step => step.id === 'rec55-first').revision,
    });
    await recommendation(page).getByRole('link', { name: 'Задать вопрос наставнику' }).waitFor();
    const revokedPlan = await api(mentor, '/mentor/interns/intern_demo_1/plan');
    await api(mentor, '/mentor/interns/intern_demo_1/steps/rec55-first/confirmation', 'PUT', {
      confirmed: true, revision: revokedPlan.steps.find(step => step.id === 'rec55-first').revision,
    });
    await recommendation(page).getByText('Шаг: «Зависимый шаг»').waitFor();
    const readyPlan = await api(page, '/onboarding/plan');
    await api(page, '/onboarding/steps/rec55-dependent/confirmation', 'PUT', {
      confirmed: true, revision: readyPlan.steps.find(step => step.id === 'rec55-dependent').revision,
    });
    await recommendation(page).getByText('Все задачи выполнены', { exact: true }).waitFor();
    const donePlan = await api(page, '/onboarding/plan');
    await api(page, '/onboarding/steps/rec55-dependent/confirmation', 'PUT', {
      confirmed: false, revision: donePlan.steps.find(step => step.id === 'rec55-dependent').revision,
    });
    await recommendation(page).getByText('Шаг: «Зависимый шаг»').waitFor();

    const hr = await login('Второй стажер');
    const hrCard = recommendation(hr);
    await hrCard.getByRole('link', { name: 'Обратиться к HR' }).waitFor();
    for (const width of [360, 768, 1440]) await picture(hr, 'hr-waiting', width);
    await hrCard.getByRole('link', { name: 'Обратиться к HR' }).click();
    const { recipient, conversation } = await askForHelp(hr, 'hr', 'rec55-hr');
    await recipient.selectOption('mentor');
    assert.equal(await recipient.inputValue(), 'mentor');
    await recipient.selectOption('hr');
    await hr.getByLabel('Контекст для специалиста').fill('Проверка адресата из рекомендации');
    await hr.getByRole('checkbox', { name: 'Включить чекпоинт: Кадровые документы', exact: true }).check();
    await hr.getByRole('checkbox', { name: 'Я проверил вопрос, адресата и весь отправляемый контекст' }).check();
    const sent = hr.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/questions'));
    await hr.getByRole('button', { name: 'Подтвердить отправку' }).click();
    const response = await sent;
    assert.equal(response.status(), 200);
    const question = await response.json();
    assert.equal(question.direction, 'hr');
    assert.equal(question.step_id, 'rec55-hr');
    await writeFile(`${directory}/state.json`, JSON.stringify({ conversation, question: question.id }));

    const noHR = await login('Стажёр без HR');
    await recommendation(noHR).getByText('В вашей команде пока нет активного HR-специалиста. Обратитесь к наставнику.').waitFor();
    assert.equal(await recommendation(noHR).getByRole('link', { name: 'Обратиться к HR' }).count(), 0);
    for (const width of [360, 768, 1440]) await picture(noHR, 'no-hr', width);
    console.log('Recommendation, dependencies, keyboard, chat recipient and missing HR passed at 360/768/1440');
  }
  assert.deepEqual(errors, [], 'Unexpected browser errors');
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
