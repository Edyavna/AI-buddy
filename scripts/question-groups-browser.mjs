import assert from 'node:assert/strict';
import { mkdir,readFile,writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
const {chromium}=await import(process.env.GROUP_PLAYWRIGHT_MODULE?pathToFileURL(process.env.GROUP_PLAYWRIGHT_MODULE).href:'playwright-core');
const origin=process.env.GROUP_APP_URL??'http://127.0.0.1:5173';
const directory=process.env.GROUP_SCREENSHOTS??'tmp/question-groups-browser';await mkdir(directory,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.GROUP_BROWSER&&{executablePath:process.env.GROUP_BROWSER})});
const run=Date.now().toString(36),reports=[];
const escape=text=>text.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
async function login(page,name){await page.goto(origin+'/login');await page.getByRole('radio',{name:new RegExp(name)}).click();await page.getByRole('button',{name:/Войти как/}).click();await page.waitForURL(url=>!url.pathname.endsWith('/login'));}
async function question(page,direction,text){
 await page.goto(origin+'/chat');await page.getByRole('textbox',{name:'Ваш вопрос'}).fill(text);await page.getByRole('button',{name:'Отправить',exact:true}).click();await page.getByRole('button',{name:'Обратиться к специалисту'}).last().click();await page.getByRole('combobox',{name:/Адресат/}).selectOption(direction);await page.getByLabel('Контекст для специалиста').fill('Личный контекст пользователя N58 — не включать в инструкцию');
 await page.getByRole('checkbox',{name:'Я проверил вопрос, адресата и весь отправляемый контекст'}).check();
 const reply=page.waitForResponse(r=>r.request().method()==='POST'&&/\/questions$/.test(new URL(r.url()).pathname));await page.getByRole('button',{name:'Подтвердить отправку'}).click();return await (await reply).json();
}
async function docForIntern(page,conversation,title){await page.goto(conversation);await page.getByRole('button',{name:'Обратиться к специалисту'}).first().click();await page.getByRole('button',{name:'Открыть инструкцию',exact:true}).last().click();await page.getByRole('dialog',{name:title,exact:true}).waitFor();await page.keyboard.press('Escape');}
// Two genuine tabs share the specialist's session but keep independent forms.
async function checkConcurrentEdits(actor,groupUrl,direction,width){
 const other=await actor.context().newPage();
 const answer=page=>page.getByLabel('Общее решение без личных подробностей');
 async function save(page){const response=page.waitForResponse(r=>r.request().method()==='PUT'&&r.url().endsWith(new URL(groupUrl).pathname));await page.getByRole('button',{name:'Сохранить состав',exact:true}).click();const result=await response;assert.equal(result.status(),200);return result.json();}
 async function reread(page){const response=page.waitForResponse(r=>r.request().method()==='GET'&&r.url().endsWith(new URL(groupUrl).pathname));await page.getByRole('button',{name:'Перечитать версию',exact:true}).click();await response;}
 try{
  await other.goto(groupUrl);await other.getByRole('heading',{name:'Состав и общее решение'}).waitFor();
  const fresh='Свежая редакция: создайте заявку и согласуйте её с ответственным специалистом.';
  await answer(other).fill(fresh);await save(other);await reread(actor);
  await actor.waitForFunction(text=>document.querySelector('textarea')?.value===text,fresh);
  assert.equal((await save(actor)).resolution,fresh);
  const local='Локальная редакция: создайте заявку и дождитесь согласования.';
  const remote='Серверная редакция: создайте заявку через портал и дождитесь согласования.';
  await answer(actor).fill(local);await reread(other);await answer(other).fill(remote);await save(other);await reread(actor);
  await actor.getByRole('region',{name:'Конфликт изменений'}).waitFor();
  assert.equal(await actor.getByRole('button',{name:'Сохранить состав',exact:true}).isDisabled(),true);
  assert.equal(await actor.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await actor.evaluate(()=>{window.scrollTo(0,0);document.activeElement?.blur();});
  await actor.screenshot({path:`${directory}/${direction}-conflict-${width}.png`,fullPage:true});
  const keepLocal=width===768;
  await actor.getByRole('button',{name:`Общее решение: ${keepLocal?'оставить мои правки':'использовать серверный вариант'}`}).focus();await actor.keyboard.press('Enter');
  assert.equal((await save(actor)).resolution,keepLocal?local:remote);
 }finally{await other.close();}
}
try{
 if(process.argv.includes('--verify')){
  const saved=JSON.parse(await readFile(`${directory}/report.json`,'utf8'));
  for(const item of saved.filter(r=>r.width===1440)){
   const actor=await browser.newPage();await login(actor,item.direction==='hr'?'Анна, HR':'Тестовый наставник');await actor.goto(item.groupUrl);await actor.getByRole('heading',{name:item.title,exact:true}).waitFor();const data=await(await actor.request.get(origin+`/api/v1/${item.direction}/question-groups/${item.id}`)).json();assert.equal(data.delivered_count,2);assert.equal(data.members.every(m=>m.status==='closed'),true);
   for(const [i,name]of ['Тестовый стажер','Второй стажер'].entries()){const intern=await browser.newPage();await login(intern,name);await docForIntern(intern,item.conversations[i],item.document_title);await intern.close();}await actor.close();console.log(`Restored ${item.direction} group and both recipients after API restart`);
  }
 }else for(const width of [360,768,1440])for(const direction of ['mentor','hr']){
  const actorContext=await browser.newContext({viewport:{width,height:960}});
  const actor=await actorContext.newPage(),first=await browser.newPage({viewport:{width,height:960}}),second=await browser.newPage({viewport:{width,height:960}});const errors=[];for(const p of [actor,first,second])p.on('pageerror',error=>errors.push(error.message));
  await login(actor,direction==='hr'?'Анна, HR':'Тестовый наставник');await login(first,'Тестовый стажер');await login(second,'Второй стажер');
  const topic=direction==='hr'?'оформить ежегодный отпуск':'оформить доступ к сервису';
  const qa=await question(first,direction,`Как ${topic} N58 ${run} ${width} A?`),qb=await question(second,direction,`Помогите ${topic} N58 ${run} ${width} B?`);const conversations=[first.url(),second.url()];
  if(direction==='hr')for(const q of [qa,qb]){await actor.goto(origin+'/hr/questions/'+q.id);await actor.getByRole('button',{name:'Взять в работу'}).click();await actor.getByLabel('Ответ стажёру').waitFor();}
  await actor.goto(origin+`/${direction}/questions`);await actor.getByRole('link',{name:'Один ответ на похожие вопросы'}).click();
  await actor.getByRole('heading',{name:'Предложения для проверки'}).waitFor();
  const title=`${direction==='hr'?'Оформить ежегодный отпуск':'Оформить доступ к сервису'} N58 ${run} ${width}`;
  await actor.getByLabel('Название темы').fill(title);await actor.getByLabel('Общее решение без личных подробностей').fill(direction==='hr'?'Подайте заявление через кадровый портал и согласуйте с руководителем.':'Создайте заявку на доступ и дождитесь согласования.');
  for(const q of [qa,qb])await actor.getByRole('checkbox',{name:new RegExp(escape(q.question))}).check();
  await actor.getByRole('button',{name:'Подтвердить группу',exact:true}).focus();await actor.keyboard.press('Enter');await actor.getByRole('heading',{name:'Состав и общее решение'}).waitFor();const groupUrl=actor.url(),id=new URL(groupUrl).pathname.split('/').at(-1);
  await actor.reload();await actor.getByRole('heading',{name:title,exact:true}).waitFor();assert.equal(await actor.getByRole('checkbox').count(),2);
  await checkConcurrentEdits(actor,groupUrl,direction,width);
  if(width===768){await actor.goto(origin+`/${direction}/questions/${qa.id}`);if(direction==='mentor'){await actor.getByRole('button',{name:'Взять в работу'}).click();}else{await actor.getByLabel('Ответ стажёру').fill('Проверяем актуальность личного обращения.');const patch=actor.waitForResponse(r=>r.request().method()==='PATCH'&&r.url().includes(qa.id));await actor.getByRole('button',{name:'Отправить ответ',exact:true}).click();await patch;}await actor.goto(groupUrl);}
  assert.equal(await actor.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await actor.screenshot({path:`${directory}/${direction}-group-${width}.png`,fullPage:true});
  await actor.getByRole('link',{name:'Подготовить общую инструкцию'}).click();await actor.getByRole('button',{name:'Предложить текст',exact:true}).click();await actor.getByText('Предложение помощника. Проверьте и отредактируйте текст.').waitFor();const markdown=actor.getByRole('textbox',{name:/^Текст инструкции/});assert.equal((await markdown.inputValue()).includes('Личный контекст'),false);
  const documentTitle=`Общая инструкция N58 ${direction} ${run} ${width}`;await actor.getByLabel('Название инструкции').fill(documentTitle);await actor.getByRole('button',{name:'Сохранить черновик',exact:true}).click();await actor.getByText('Все изменения сохранены',{exact:true}).waitFor();await actor.reload();await actor.getByRole('checkbox',{name:/Я проверил текст и аудиторию/}).check();await actor.getByRole('button',{name:'Опубликовать инструкцию',exact:true}).focus();await actor.keyboard.press('Enter');await actor.getByRole('button',{name:'Открыть опубликованную инструкцию'}).waitFor();await actor.getByRole('link',{name:'← К группе вопросов'}).click();
  await actor.getByRole('button',{name:'Доставить или повторить ссылки'}).waitFor();const dataBefore=await(await actor.request.get(origin+`/api/v1/${direction}/question-groups/${id}`)).json();assert.equal(dataBefore.members.every(m=>m.status==='pending'),true);
  const delivery=actor.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/deliver'));await actor.getByRole('button',{name:'Доставить или повторить ссылки'}).click();let data=await(await delivery).json();
  if(width===768){assert.equal(data.delivered_count,1);assert.equal(data.members.some(m=>m.status==='failed'),true);const save=actor.waitForResponse(r=>r.request().method()==='PUT'&&r.url().endsWith('/question-groups/'+id));await actor.getByRole('button',{name:'Сохранить состав',exact:true}).click();await save;const retry=actor.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/deliver'));await actor.getByRole('button',{name:'Доставить или повторить ссылки'}).click();data=await(await retry).json();}
  assert.equal(data.delivered_count,2);assert.equal(data.members.every(m=>m.question.status!=='resolved'),true);
  const close=actor.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/close'));await actor.getByRole('button',{name:'Закрыть доставленные вопросы'}).focus();await actor.keyboard.press('Enter');data=await(await close).json();assert.equal(data.members.every(m=>m.status==='closed'),true);
  assert.equal((await first.request.get(origin+`/api/v1/${direction}/question-groups/${id}`)).status(),403);assert.equal((await first.request.get(origin+'/api/v1/questions/'+qb.id)).status(),404);assert.equal((await second.request.get(origin+'/api/v1/questions/'+qa.id)).status(),404);
  await docForIntern(first,conversations[0],documentTitle);await docForIntern(second,conversations[1],documentTitle);
  if(direction==='hr'){await first.getByRole('textbox',{name:'Ваш вопрос'}).fill('Где описан порядок оформления ежегодного отпуска?');await first.getByRole('button',{name:'Отправить',exact:true}).click();await first.getByRole('list',{name:'Источники ответа'}).last().getByText(/Версия 1/).waitFor();}
  assert.equal(await first.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);assert.deepEqual(errors,[]);await first.screenshot({path:`${directory}/${direction}-recipient-${width}.png`,fullPage:true});
  reports.push({width,direction,id,groupUrl,title,document_title:documentTitle,document_id:data.draft.document_id,conversations,question_ids:[qa.id,qb.id],partial_retry:width===768,two_tab_conflicts:true});await writeFile(`${directory}/report.json`,JSON.stringify(reports,null,2));await Promise.all([actorContext.close(),first.close(),second.close()]);console.log(`Shared answer passed ${direction} ${width}px: two-tab conflicts, both recipients, closure and isolation`);
 }
}catch(error){for(const [i,context]of browser.contexts().entries())for(const page of context.pages()){console.error(`Failure ${i}: ${page.url()}\n${await page.locator('body').innerText()}`);await page.screenshot({path:`${directory}/failure-${i}.png`,fullPage:true});}throw error;}finally{await browser.close();}
