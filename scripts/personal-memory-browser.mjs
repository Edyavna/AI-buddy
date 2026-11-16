// Real SPA, cookie sessions, Go and PostgreSQL. Only the external provider is
// deterministic; the browser never intercepts application requests.
import assert from 'node:assert/strict';
import { mkdir,readFile,writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const {chromium}=await import(process.env.MEMORY_PLAYWRIGHT_MODULE?pathToFileURL(process.env.MEMORY_PLAYWRIGHT_MODULE).href:'playwright-core');
const origin=process.env.MEMORY_APP_URL??'http://localhost:5173';
const directory=process.env.MEMORY_SCREENSHOTS??'tmp/personal-memory-browser';
await mkdir(directory,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.MEMORY_BROWSER&&{executablePath:process.env.MEMORY_BROWSER})});
const run=Date.now().toString(36),report=[];
async function login(page,name){await page.goto(origin+'/login');await page.getByRole('radio',{name:new RegExp(name)}).click();await page.getByRole('button',{name:/Войти как/}).click();await page.waitForURL(url=>!url.pathname.endsWith('/login'));}
async function json(page,path){const response=await page.request.get(origin+'/api/v1'+path);assert.equal(response.status(),200,path+': '+await response.text());return response.json();}
async function capture(page,name){
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.evaluate(()=>{window.scrollTo(0,0);document.activeElement?.blur();});
  if(await page.locator('[data-chat-layout]').count()) {
    assert.equal(await page.getByRole('textbox',{name:'Ваш вопрос'}).evaluate(input=>{
      const send=input.closest('form').querySelector('button[type="submit"]');
      const log=document.querySelector('[role="log"]');
      return input.getBoundingClientRect().bottom<=innerHeight&&send.getBoundingClientRect().bottom<=innerHeight&&log.getBoundingClientRect().height>0;
    }),true,'Memory proposals must leave the composer and scrollable history visible');
  }
  await page.screenshot({path:`${directory}/${name}.png`,fullPage:true,animations:'disabled'});
}
async function send(page,text){
  await page.getByRole('textbox',{name:'Ваш вопрос'}).fill(text);
  await page.getByRole('button',{name:'Отправить',exact:true}).click();
}
async function ask(page,text){
  await page.goto(origin+'/chat');await send(page,text);
  await page.waitForURL(url=>/^\/chat\/[^/]+$/.test(url.pathname));
  const url=page.url(),id=new URL(url).pathname.split('/').at(-1);
  await page.waitForFunction(async id=>{
    const response=await fetch(`/api/v1/conversations/${id}/messages`);
    const messages=response.ok?await response.json():[];
    return messages.at(-1)?.role==='assistant'&&messages.at(-1)?.status==='completed';
  },id,{polling:100});
  const messages=await json(page,`/conversations/${id}/messages`);
  assert.equal(messages.at(-1).status,'completed');
  assert.equal((await json(page,`/conversations/${id}`)).title,'Личный диалог','Memory must preserve the configured title generator');
  return {url,message:messages.find(m=>m.role==='user')};
}
async function confirmPowerShell(page){
  const button=page.getByRole('button',{name:'Запомнить: Использую PowerShell',exact:true});await button.waitFor();
  const response=page.waitForResponse(r=>r.request().method()==='PUT'&&r.url().includes('/memory/proposals/'));
  await button.focus();await page.keyboard.press('Enter');const result=await response;assert.equal(result.status(),200,await result.text());
  await page.getByText(/Предложение подтверждено/).waitFor();return result.json();
}
async function clear(page){
  await page.goto(origin+'/memory');await page.getByRole('heading',{name:'Сохранённые предпочтения',exact:true}).waitFor();
  while(await page.getByRole('button',{name:/^Не сохранять$/}).count()){const before=await page.getByRole('button',{name:'Не сохранять',exact:true}).count();await page.getByRole('button',{name:'Не сохранять',exact:true}).first().click();await page.waitForFunction(expected=>{const section=document.querySelector('section[aria-label="Ожидают подтверждения"]');return section&&section.querySelectorAll('article').length===expected;},before-1);}
  while(await page.getByRole('button',{name:/^Удалить: /}).count()){
    const before=await page.getByRole('button',{name:/^Удалить: /}).count();await page.getByRole('button',{name:/^Удалить: /}).first().click();const removing=page.waitForResponse(r=>r.request().method()==='DELETE'&&r.url().includes('/memory/entries/'));await page.getByRole('button',{name:'Удалить из памяти',exact:true}).click();assert.equal((await removing).status(),200);await page.waitForFunction(expected=>{const section=document.querySelector('section[aria-label="Сохранённые предпочтения"]');return section&&section.querySelectorAll('article').length===expected;},before-1);
  }
}
try{
  if(process.argv.includes('--verify')){
    const saved=JSON.parse(await readFile(`${directory}/report.json`,'utf8')).find(row=>row.width===1440);
    const intern=await browser.newPage(),peer=await browser.newPage();await login(intern,'Тестовый стажер');
    await intern.goto(origin+'/memory');await intern.getByRole('heading',{name:'Использую PowerShell',exact:true}).waitFor();
    const workspace=await json(intern,'/memory');assert.equal(workspace.entries.length,1);assert.equal(workspace.entries[0].id,saved.persisted_entry);
    const expected=process.env.MEMORY_EXPECT_MENTOR??'Второй наставник';assert.equal(workspace.context.mentor.name,expected);
    await ask(intern,'Кто мой наставник? N79 restart '+run);await intern.getByText('Текущий наставник: '+expected,{exact:true}).waitFor();
    await ask(intern,'Как проверить текущую папку? N79 restart '+run);await intern.locator('code').filter({hasText:'Get-Location'}).waitFor();
    await login(peer,'Второй стажер');assert.equal((await peer.request.get(origin+'/api/v1/memory/entries/'+saved.persisted_entry)).status(),404);assert.equal((await json(peer,'/memory')).entries.length,0);
    await ask(peer,'Как проверить текущую папку? N79 isolated '+run);await peer.getByText('Уточните рабочую оболочку; предпочтения пока не сохранены.',{exact:true}).waitFor();
    await capture(intern,'restored-context');await Promise.all([intern.close(),peer.close()]);console.log('Memory restored after restart/reseed; new mentor is loaded and peer context stays isolated');
  }else for(const width of [360,768,1440]){
    const context=await browser.newContext({viewport:{width,height:960}}),intern=await context.newPage(),peer=await browser.newPage({viewport:{width,height:960}});
    const errors=[];intern.on('pageerror',error=>errors.push(error.message));peer.on('pageerror',error=>errors.push(error.message));
    await login(intern,'Тестовый стажер');await login(peer,'Второй стажер');await clear(intern);
    const source=await ask(intern,`Я работаю на Windows и использую PowerShell. N79 ${run} ${width}`);
    await intern.getByRole('heading',{name:'Запомнить для следующих диалогов?',exact:true}).waitFor();
    assert.equal((await json(intern,'/memory')).entries.length,0);
    await capture(intern,`proposals-${width}`);
    const os=intern.getByRole('article',{name:'Работаю на Windows',exact:true});await os.getByRole('button',{name:'Не сохранять',exact:true}).click();await os.getByText('Предложение отклонено.',{exact:true}).waitFor();
    const accepted=await confirmPowerShell(intern);assert.equal(accepted.entry.value,'powershell');
    await intern.reload();await intern.getByText(/Предложение подтверждено/).waitFor();assert.equal((await json(intern,'/memory')).entries.length,1);
    // Replay is an ordinary API confirmation of the same already approved ID.
    const replay=await intern.request.put(origin+'/api/v1/memory/proposals/'+accepted.proposal.id,{headers:{Origin:origin},data:{decision:'accept',expected_entry_id:null,expected_revision:0}});assert.equal(replay.status(),200);assert.equal((await replay.json()).entry.id,accepted.entry.id);
    await ask(intern,`Как проверить текущую папку? N79 ${run} ${width}`);await intern.locator('code').filter({hasText:'Get-Location'}).waitFor();
    await capture(intern,`powershell-answer-${width}`);
    assert.equal((await peer.request.get(origin+'/api/v1/memory/entries/'+accepted.entry.id)).status(),404);
    assert.equal((await peer.request.get(origin+'/api/v1/messages/'+source.message.id+'/memory-proposals')).status(),404);
    await ask(peer,`Как проверить текущую папку? N79 ${run} ${width}`);await peer.getByText('Уточните рабочую оболочку; предпочтения пока не сохранены.',{exact:true}).waitFor();
    if(width===360){
      const failed=await ask(intern,`Я изучаю Docker. N79 fail ${run}`);await intern.getByText('Не удалось подобрать факты. Память не изменена; можно продолжать разговор.',{exact:true}).waitFor();assert.equal((await json(intern,'/messages/'+failed.message.id+'/memory-proposals')).state,'failed');
      const sensitive=await ask(intern,`Мой токен test_placeholder_only. Я изучаю Docker. N79 ${run}`);await intern.waitForFunction(async path=>(await(await fetch('/api/v1'+path)).json()).state==='skipped','/messages/'+sensitive.message.id+'/memory-proposals');assert.equal((await json(intern,'/messages/'+sensitive.message.id+'/memory-proposals')).state,'skipped');assert.equal((await json(intern,'/memory')).entries.length,1);
    }
    if(width===1440){
      const ambiguous=await ask(intern,`У коллеги Windows, мне тоже удобно. N79 ${run}`);
      const card=intern.getByRole('article',{name:'Работаю на Windows',exact:true});
      await card.getByText('Возможно, это ваше предпочтение',{exact:true}).waitFor();
      const before=(await json(intern,`/messages/${ambiguous.message.id}/memory-proposals`)).items[0];
      await send(intern,'Запомни это');
      await intern.getByText(/Нажмите «Запомнить», чтобы добавить его в память/).waitFor();
      const conversationId=new URL(ambiguous.url).pathname.split('/').at(-1);
      const messages=await json(intern,`/conversations/${conversationId}/messages`);
      const followUp=messages.find(m=>m.role==='user'&&m.text==='Запомни это');
      const path=`/messages/${followUp.id}/memory-proposals`;
      await intern.waitForFunction(async path=>(await(await fetch('/api/v1'+path)).json()).state==='ready',path,{polling:100});
      const [after]=(await json(intern,path)).items;
      assert.equal(after.id,before.id);
      assert.equal(after.source_message_id,ambiguous.message.id);
      assert.equal(await card.getByRole('link',{name:'Исходная реплика'}).getAttribute('href'),`/chat/${conversationId}#message-${ambiguous.message.id}`);
      assert.equal((await json(intern,'/memory')).entries.length,1);
      await card.getByRole('button',{name:'Не сохранять',exact:true}).click();
      await card.getByText('Предложение отклонено.',{exact:true}).waitFor();
    }
    await intern.goto(origin+'/memory');await intern.getByRole('heading',{name:'Использую PowerShell',exact:true}).waitFor();
    await capture(intern,`memory-${width}`);
    if(width===1440){const popup=intern.waitForEvent('popup');await intern.getByRole('link',{name:'Исходная реплика',exact:true}).click();const original=await popup;await original.waitForURL(url=>url.hash==='#message-'+source.message.id);await original.locator('[id="message-'+source.message.id+'"]').waitFor();await original.close();}
    await intern.getByRole('button',{name:'Изменить: Использую PowerShell',exact:true}).focus();await intern.keyboard.press('Enter');
    await intern.getByRole('combobox',{name:'Новое предпочтение'}).selectOption('bash');await capture(intern,`edit-${width}`);
    await intern.getByRole('button',{name:'Сохранить предпочтение',exact:true}).focus();await intern.keyboard.press('Enter');await intern.getByRole('heading',{name:'Использую Bash',exact:true}).waitFor();
    await ask(intern,`Как проверить текущую папку? N79 bash ${run} ${width}`);await intern.locator('code').filter({hasText:/^pwd$/}).waitFor();
    await intern.goto(origin+'/memory');await intern.getByRole('button',{name:'Удалить: Использую Bash',exact:true}).click();await intern.getByRole('button',{name:'Удалить из памяти',exact:true}).focus();await intern.keyboard.press('Enter');
    await intern.getByText('Вы ещё ничего не сохранили. Предложения становятся памятью только после вашего подтверждения.',{exact:true}).waitFor();
    const noResurrection=await intern.request.put(origin+'/api/v1/memory/proposals/'+accepted.proposal.id,{headers:{Origin:origin},data:{decision:'accept',expected_entry_id:null,expected_revision:0}});assert.equal(noResurrection.status(),404);const receipt=await intern.request.get(origin+'/api/v1/memory/entries/'+accepted.entry.id);assert.equal((await receipt.json()).error.code,'MEMORY_ENTRY_DELETED');
    await ask(intern,`Как проверить текущую папку? N79 deleted ${run} ${width}`);await intern.getByText('Уточните рабочую оболочку; предпочтения пока не сохранены.',{exact:true}).waitFor();
    let persisted=null;if(width===1440){await ask(intern,`Я использую PowerShell. N79 persist ${run}`);persisted=(await confirmPowerShell(intern)).entry.id;}
    assert.deepEqual(errors,[]);report.push({width,source:source.url,source_message_id:source.message.id,persisted_entry:persisted,confirmed:true,edited:true,deleted:true,isolated:true});await writeFile(`${directory}/report.json`,JSON.stringify(report,null,2));
    await Promise.all([context.close(),peer.close()]);console.log(`Personal memory ${width}px passed: proposal, rejection, replay, new-dialog command, edit, delete and isolation`);
  }
}catch(error){for(const [i,context]of browser.contexts().entries())for(const page of context.pages()){console.error(`Failure ${i}: ${page.url()}\n${await page.locator('body').innerText()}`);await page.screenshot({path:`${directory}/failure-${i}.png`,fullPage:true});}throw error;}finally{await browser.close();}
