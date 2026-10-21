// Windows browser acceptance tests. All data is isolated from the user's blog.
const { chromium } = require('../../web/node_modules/playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const { spawn, execFileSync } = require('node:child_process');
const root = path.resolve(__dirname, '../..');
const artifacts = process.env.E2E_ARTIFACTS_DIR
  ? path.resolve(process.env.E2E_ARTIFACTS_DIR)
  : path.join(root, '.e2e-artifacts');
const results = [];
let server, browser, page, base, scratch;
const errors = [];
const contexts = [];

async function freePort() {
  const socket = net.createServer();
  await new Promise((resolve, reject) => socket.listen(0, '127.0.0.1', resolve).once('error', reject));
  const port = socket.address().port;
  await new Promise(resolve => socket.close(resolve));
  return port;
}
async function start(data, media = path.join(data, 'uploads')) {
  const port = await freePort();
  base = `http://127.0.0.1:${port}`;
  server = spawn(path.join(scratch, 'server.exe'), [], {cwd: root, windowsHide: true,
    env: {...process.env, APP_PORT: String(port), APP_DATA_DIR: data, APP_MEDIA_DIR: media,
      APP_WEB_DIR: path.join(root, 'web/dist'), APP_LISTEN_HOST: '', APP_COOKIE_SECURE: 'false', APP_COMMENT_RATE_LIMIT: '200', APP_VERSION: 'e2e'}});
  const log = require('node:fs').createWriteStream(path.join(artifacts, 'server.log'), {flags:'a'});
  server.stdout.pipe(log, {end:false}); server.stderr.pipe(log, {end:false});
  server.once('close', () => log.end());
  let spawnError; server.once('error', error => {spawnError=error;});
  for (let attempt=0; attempt<100; attempt++) {
    if (spawnError) throw spawnError;
    if (server.exitCode !== null) throw new Error(`Server exited: ${server.exitCode}`);
    try {if ((await fetch(base+'/healthz')).ok) return;} catch {}
    await new Promise(resolve=>setTimeout(resolve,100));
  }
  throw new Error('Server did not become live');
}
async function stop() {
  if (server && server.exitCode === null) {
    const exited = new Promise(resolve=>server.once('exit',resolve));
    server.kill(); await exited;
  }
}
async function context() {
  const ctx = await browser.newContext({acceptDownloads:true}); contexts.push(ctx);
  ctx.on('page', p=>{p.setDefaultTimeout(10000);p.on('pageerror',e=>errors.push(e.message));});
  return ctx;
}
async function api(ctx, method, route, data, expected=200, csrf=true) {
  const token = (await ctx.cookies(base)).find(c=>c.name==='app_csrf')?.value;
  const response = await ctx.request.fetch(base+route,{method,data,
    headers:csrf&&token?{'X-CSRF-Token':token}:{}});
  assert.equal(response.status(),expected,`${method} ${route}: ${await response.text()}`);
  return response.status()===204 ? null : response.json();
}
function parseColor(value) {
  const match = String(value).match(/rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/);
  assert.ok(match, 'unparsed color ' + value);
  return match.slice(1, 4).map(Number);
}
function assertNeutralGround(value) {
  const [r, g, b] = parseColor(value);
  const spread = Math.max(r, g, b) - Math.min(r, g, b);
  assert.ok(spread <= 6, `page ground is tinted (${value})`);
  assert.ok(r > 220 && g > 220 && b > 220, `page ground is too dark (${value})`);
  assert.ok(!(r === 251 && g === 246 && b === 239), 'old cream paper is still the page ground');
}
function assertCompactRadius(value, label) {
  assert.equal(typeof value, 'string', label + ' is missing');
  for (const part of value.trim().split(/\s+/)) {
    assert.match(part, /^\d+(?:\.\d+)?px$/, `${label} radius ${value}`);
    assert.ok(Number.parseFloat(part) <= 8, `${label} radius ${value} exceeds 8px`);
  }
}
async function sampleChrome(p, suffix) {
  const routes = [
    {path: '/user', button: 'button.button', input: 'input', card: '.list-mast, .post-card, .empty-state', avatar: '.brand-mark'},
    {path: '/admin', button: 'button.button, button.avatar', input: 'input, select, textarea', card: '.stat-card', avatar: '.avatar'},
    {path: '/setting', button: 'button.button', input: 'input, textarea, select', card: '.settings-block', avatar: '.brand-mark'},
  ];
  const samples = [];
  for (const route of routes) {
    await p.goto(base + route.path, {waitUntil: 'domcontentloaded'});
    await p.locator('h1, h2').first().waitFor();
    const sample = await p.evaluate(({button, input, card, avatar}) => {
      const first = (sel) => document.querySelector(sel);
      const box = (el) => {
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return {w: r.width, h: r.height, top: r.top, bottom: r.bottom};
      };
      const radius = (el) => el ? getComputedStyle(el).borderRadius : null;
      const heading = document.querySelector('h1, h2');
      const form = document.querySelector('form');
      const inView = (b) => !!(b && b.w > 40 && b.h > 16 && b.top < window.innerHeight * 0.92 && b.bottom > 0);
      return {
        ground: getComputedStyle(document.body).backgroundColor,
        backgroundImage: getComputedStyle(document.body).backgroundImage,
        filled: inView(box(heading)) || inView(box(form)),
        headingText: heading ? heading.textContent.trim().slice(0, 48) : '',
        radii: {
          button: radius(first(button)),
          input: radius(first(input)),
          card: radius(first(card)),
          avatar: radius(first(avatar)),
        },
      };
    }, route);
    samples.push({path: route.path, ...sample});
    await p.screenshot({path: path.join(artifacts, `visual-${suffix}-${route.path.replace(/\W+/g, '') || 'root'}.png`), fullPage: false});
  }
  return samples;
}
function chromeSignature(samples) {
  return samples.map((sample) => ({path: sample.path, ground: sample.ground, backgroundImage: sample.backgroundImage, filled: sample.filled, radii: sample.radii}));
}
async function step(name, work) {
  const started=Date.now();
  try {await work();results.push({name,status:'passed',durationMs:Date.now()-started});console.log('PASS '+name);}
  catch(error) {results.push({name,status:'failed',error:error.stack,durationMs:Date.now()-started});throw error;}
}
async function login(ctx, username, password) {
  const p = await ctx.newPage(); await p.goto(base+'/admin');
  await p.getByLabel('用户名',{exact:true}).fill(username);
  await p.getByLabel('密码',{exact:true}).fill(password);
  await p.getByRole('button',{name:'登录管理端',exact:true}).click();
  await p.locator('.stats-grid').waitFor(); return p;
}
function postRow(title) {return page.locator('#posts .admin-post-row').filter({hasText:title});}
async function openAdminSection(name) {
  if (new URL(page.url()).pathname !== '/admin') await page.goto(base+'/admin');
  const link=page.locator('.command-nav').getByRole('link',{name,exact:true});
  if(await link.getAttribute('aria-current')!=='page')await link.click();
}
async function writePost(title,slug,content,status='published') {
  await openAdminSection('文章');
  const existingEditor=page.locator('#post-editor');
  const posts=page.locator('#posts');
  if(await existingEditor.isVisible())await posts.getByRole('button',{name:'关闭编辑器',exact:true}).click();
  await posts.getByRole('button',{name:'+ 写文章',exact:true}).click();
  await page.getByPlaceholder('文章标题',{exact:true}).fill(title);
  await page.getByPlaceholder('留空则保留现有地址或按标题生成').fill(slug);
  await page.getByPlaceholder('开始写作，支持 Markdown…').fill(content);
  await page.locator('#posts select').selectOption(status);
  await page.getByRole('button',{name:'保存文章',exact:true}).click();
  await postRow(title).waitFor();
}

(async()=>{
  assert.equal(process.platform,'win32','These tests support Windows only');
  await fs.mkdir(artifacts,{recursive:true});
  scratch=await fs.mkdtemp(path.join(os.tmpdir(),'typecho-e2e-'));
  console.log('Temporary data: '+scratch);
  execFileSync('powershell.exe',['-NoProfile','-Command','npm.cmd run build'],{cwd:path.join(root,'web'),stdio:'inherit',windowsHide:true});
  execFileSync('go',['build','-o',path.join(scratch,'server.exe'),'./cmd/server'],{cwd:root,stdio:'inherit',windowsHide:true,env:{...process.env,CGO_ENABLED:'1'}});
  const executablePath=process.env.E2E_BROWSER_PATH || path.join(process.env['ProgramFiles(x86)']||'C:/Program Files (x86)','Microsoft/Edge/Application/msedge.exe');
  browser=await chromium.launch({executablePath,headless:true});
  const dataDir=path.join(scratch,'data');await start(dataDir);
  const owner=await context();page=await owner.newPage();page.on('dialog',d=>d.accept());
  const publicCtx=await context();const reader=await publicCtx.newPage();
  let originalPost, uploaded, backupPath;
  await step('first setup and readiness',async()=>{
    assert.equal((await fetch(base+'/readyz')).status,503);
    await page.goto(base);await page.waitForURL('**/setting');
    await page.getByLabel('站点名称',{exact:true}).fill('E2E 个人博客');
    await page.getByLabel('站点地址',{exact:true}).fill(base);
    await page.getByLabel('管理员邮箱',{exact:true}).fill('owner@example.com');
    await page.getByLabel('用户名',{exact:true}).fill('owner');
    await page.getByLabel('密码',{exact:true}).fill('password-owner-123');
    const passwordConfirmation = page.getByLabel('确认密码',{exact:true});
    await passwordConfirmation.fill('different-password-123');
    await page.getByText('两次输入的密码不一致',{exact:true}).waitFor();
    assert.equal(await passwordConfirmation.getAttribute('aria-invalid'),'true');
    await page.getByRole('button',{name:'完成设置并进入管理端'}).click();
    assert.equal(new URL(page.url()).pathname,'/setting','setup must reject mismatched passwords');
    await passwordConfirmation.fill('password-owner-123');
    await page.getByRole('button',{name:'完成设置并进入管理端'}).click();await page.waitForURL('**/admin');
    await page.locator('.stats-grid').waitFor();assert.equal((await fetch(base+'/readyz')).status,200);
  });
  await step('empty overview quickstart opens a blank post editor',async()=>{
    await page.goto(base+'/admin');
    await page.locator('.overview-quickstart').getByRole('button',{name:'写第一篇文章',exact:true}).click();
    await page.waitForURL('**/admin#posts');
    await page.locator('#post-editor').waitFor();
    assert.equal(await page.getByPlaceholder('文章标题',{exact:true}).inputValue(),'');
    assert.equal(await page.locator('#posts .section-count').innerText(),'0 篇');
    assert.equal((await api(owner,'GET','/api/v1/content/posts?includeDraft=true')).total,0,'opening the editor must not create a post');
  });
  await step('admin and settings routes match complete path segments',async()=>{
    for(const route of ['/administrator','/settings']){
      const response=await reader.goto(base+route);
      assert.equal(response?.status(),404,`${route} must not resolve as a managed route`);
      assert.equal(reader.url(),base+route,`${route} must not redirect to a managed route`);
    }
  });
  await step('compact chrome on reader, admin and settings',async()=>{
    const before = errors.length;
    const first = await sampleChrome(page, 'a');
    const second = await sampleChrome(page, 'b');
    assert.deepEqual(chromeSignature(second), chromeSignature(first), 'two visual probes disagreed');
    for (const sample of first) {
      assert.equal(sample.filled, true, sample.path + ' viewport is blank');
      assert.equal(sample.backgroundImage, 'none', sample.path + ' still has a paper gradient');
      assertNeutralGround(sample.ground);
      for (const key of ['button', 'input', 'card', 'avatar']) assertCompactRadius(sample.radii[key], sample.path + ' ' + key);
    }
    assert.equal(errors.length, before, 'page errors during visual probe: ' + errors.slice(before).join('; '));
    console.log('VISUAL ' + JSON.stringify(chromeSignature(first)));
  });
  if (process.env.E2E_ONLY === 'visual') return;
  await step('settings navigation, saved state, leave warning and mobile layout',async()=>{
    await page.goto(base+'/setting');const save=page.getByRole('button',{name:'保存设置',exact:true});await save.waitFor();assert.ok(await save.isDisabled());
    assert.equal(await page.getByLabel('管理员邮箱',{exact:true}).inputValue(),'owner@example.com','owner settings must load the private site email through the owner-only endpoint');
    await page.getByLabel('站点描述',{exact:true}).fill('E2E 站点介绍');assert.ok(await save.isEnabled());await page.locator('#site').getByText('有未保存的修改',{exact:true}).waitFor();
    await page.locator('.settings-index').getByRole('link',{name:'阅读与评论',exact:true}).click();assert.equal(await page.getByLabel('站点描述',{exact:true}).inputValue(),'E2E 站点介绍');
    await page.locator('.settings-index').getByRole('link',{name:'站点信息',exact:true}).click();await save.click();await page.getByText('设置已保存',{exact:true}).waitFor();assert.ok(await save.isDisabled());
    await page.reload();assert.equal(await page.getByLabel('站点描述',{exact:true}).inputValue(),'E2E 站点介绍');
    await page.setViewportSize({width:390,height:844});await page.locator('.settings-index').getByRole('link',{name:'阅读与评论',exact:true}).click();
    await page.waitForFunction(()=>Math.abs(document.getElementById('reading').getBoundingClientRect().top-128)<4);
    assert.ok(await page.evaluate(()=>{const target=document.getElementById('reading').getBoundingClientRect();const header=document.querySelector('.settings-bar').getBoundingClientRect();const nav=document.querySelector('.settings-index').getBoundingClientRect();return target.top>=Math.max(header.bottom,nav.bottom)+4;}),'mobile settings section should clear the sticky header and navigation');
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'mobile settings overflow');
    assert.equal(await page.locator('.settings-index').evaluate(el=>getComputedStyle(el).position),'sticky');
    await page.screenshot({path:path.join(artifacts,'mobile-settings.png'),fullPage:false});await page.setViewportSize({width:1280,height:800});
    await page.locator('.settings-index').getByRole('link',{name:'站点信息',exact:true}).click();await page.waitForFunction(()=>Math.abs(document.getElementById('site').getBoundingClientRect().top-84)<4);
    assert.ok(await page.evaluate(()=>document.getElementById('site').getBoundingClientRect().top>=document.querySelector('.settings-bar').getBoundingClientRect().bottom+4),'settings section should clear the sticky header');
    await page.screenshot({path:path.join(artifacts,'settings.png'),fullPage:false});
    let warned=false;const onDialog=d=>{if(d.type()==='beforeunload')warned=true;};page.on('dialog',onDialog);
    await page.getByLabel('站点描述',{exact:true}).fill('Unsaved text');await page.locator('.settings-links').getByRole('link',{name:'管理',exact:true}).click();await page.waitForURL('**/admin');page.off('dialog',onDialog);assert.ok(warned,'unsaved settings must warn before leaving');
    await reader.goto(base+'/setting');await reader.getByRole('heading',{name:'请先登录',exact:true}).waitFor();await reader.getByRole('link',{name:'前往登录',exact:true}).waitFor();
  });
  await step('publish Markdown and sanitize HTML',async()=>{
    await writePost('E2E 公开文章','e2e-public','# 正文标题\n\nOriginal body\n\n<script>window.e2eXss=true</script><img src=x onerror="window.e2eXss=true">');
    originalPost=await api(owner,'GET','/api/v1/content/posts/e2e-public');
    await reader.goto(base+'/user/e2e-public');await reader.getByRole('heading',{name:'正文标题',exact:true}).waitFor();
    assert.equal(await reader.evaluate(()=>window.e2eXss),undefined);
    assert.equal(await reader.locator('.article-body [onerror], .article-body script').count(),0);
  });
  await step('custom fields save, render as text, edit and restore with revisions',async()=>{
    const injected='<img src=x onerror="window.e2eXss=true">';
    await api(owner,'POST','/api/v1/content/posts',{title:'E2E 自定义字段',slug:'e2e-custom-fields',content:'Fields body',status:'published',customFields:{caption:'Original caption',markup:injected}},201);
    const list=await api(publicCtx,'GET','/api/v1/content/posts?q='+encodeURIComponent('E2E 自定义字段'));
    const summary=list.items.find(post=>post.slug==='e2e-custom-fields');assert.ok(summary);assert.equal('customFields' in summary,false);
    await reader.goto(base+'/user/e2e-custom-fields');
    const renderedFields=reader.locator('.post-custom-fields');await renderedFields.getByText('Original caption',{exact:true}).waitFor();
    assert.ok((await renderedFields.innerText()).includes(injected));assert.equal(await renderedFields.locator('img').count(),0);assert.equal(await reader.evaluate(()=>window.e2eXss),undefined);
    await page.reload();await openAdminSection('文章');await postRow('E2E 自定义字段').getByRole('button',{name:'编辑',exact:true}).click();
    const rows=page.locator('#post-editor .custom-field-row');await rows.nth(0).locator('textarea').fill('Updated caption');
    await page.getByRole('button',{name:'添加字段',exact:true}).click();await rows.nth(2).locator('input').fill('second');await rows.nth(2).locator('textarea').fill('Added value');
    await page.getByRole('button',{name:'保存修改',exact:true}).click();await postRow('E2E 自定义字段').waitFor();
    let detail=await api(publicCtx,'GET','/api/v1/content/posts/e2e-custom-fields');assert.equal(detail.customFields.caption,'Updated caption');assert.equal(detail.customFields.second,'Added value');
    await postRow('E2E 自定义字段').getByRole('button',{name:'编辑',exact:true}).click();
    await page.getByRole('button',{name:'恢复此版本',exact:true}).first().click();
    await page.waitForFunction(()=>Array.from(document.querySelectorAll('#post-editor .custom-field-row textarea')).some(field=>field.value==='Original caption'));
    detail=await api(publicCtx,'GET','/api/v1/content/posts/e2e-custom-fields');assert.equal(detail.customFields.caption,'Original caption');assert.equal(detail.customFields.second,undefined);
  });
  await step('draft privacy and authorized preview',async()=>{
    await writePost('E2E 草稿','e2e-draft','Private draft','draft');
    await api(publicCtx,'GET','/api/v1/content/posts/e2e-draft',undefined,404);
    await reader.goto(base+'/user/e2e-draft');await reader.getByText('Private draft',{exact:true}).waitFor({state:'hidden'});
    const preview=await owner.newPage();await preview.goto(base+'/user/e2e-draft?preview=true');
    await preview.locator('.article-body').filter({hasText:'Private draft'}).waitFor();await preview.close();
  });
  await step('edit preserves permalink and restore revision',async()=>{
    await openAdminSection('文章');await postRow('E2E 公开文章').getByRole('button',{name:'编辑',exact:true}).click();
    await page.getByPlaceholder('文章标题',{exact:true}).fill('E2E 修改文章');
    await page.getByPlaceholder('开始写作，支持 Markdown…').fill('Revised body');
    await page.getByRole('button',{name:'保存修改',exact:true}).click();await postRow('E2E 修改文章').waitFor();
    assert.equal((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public')).content,'Revised body');
    await postRow('E2E 修改文章').getByRole('button',{name:'编辑',exact:true}).click();
    await page.getByRole('button',{name:'恢复此版本',exact:true}).first().click();
    await page.waitForFunction(()=>document.querySelector('#posts textarea[required]')?.value.includes('Original body'));
    assert.ok((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public')).content.includes('Original body'));
  });
  await step('page create, edit, public reading and delete',async()=>{
    await openAdminSection('页面');await page.getByRole('button',{name:'+ 新建页面',exact:true}).click();
    await page.getByPlaceholder('页面标题',{exact:true}).fill('E2E 关于');
    await page.locator('#pages .editor-slug input').fill('e2e-about');
    await page.getByPlaceholder('页面内容，支持 Markdown…').fill('About version one');
    await page.locator('#pages select').selectOption('published');await page.getByRole('button',{name:'保存页面',exact:true}).click();
    const row=page.locator('#pages .admin-post-row').filter({hasText:'E2E 关于'});await row.waitFor();
    await reader.goto(base+'/user');await reader.locator('.site-nav > a[href="/user"].active').waitFor();
    await reader.goto(base+'/page/e2e-about');await reader.locator('.article-body').filter({hasText:'About version one'}).waitFor();
    const activePageLink=reader.locator('.site-pages-panel a[href="/page/e2e-about"]');await activePageLink.waitFor({state:'attached'});
    assert.ok(await activePageLink.evaluate(el=>el.classList.contains('active')),'current page should be active in reader navigation');
    assert.equal(await reader.locator('.site-nav > a[href="/user"]').evaluate(el=>el.classList.contains('active')),false,'article navigation should not stay active on a page');
    await row.getByRole('button',{name:'编辑',exact:true}).click();await page.getByPlaceholder('页面内容，支持 Markdown…').fill('About version two');
    await page.locator('#pages').getByRole('button',{name:'保存修改',exact:true}).click();
    await reader.reload();await reader.locator('.article-body').filter({hasText:'About version two'}).waitFor();
    await row.getByRole('button',{name:'删除',exact:true}).click();await row.waitFor({state:'detached'});
    await api(publicCtx,'GET','/api/v1/content/pages/e2e-about',undefined,404);
  });
  await step('taxonomy rename keeps article associations',async()=>{
    const currentPost=await api(owner,'GET','/api/v1/content/posts/e2e-public');
    await api(owner,'PUT','/api/v1/content/posts/'+originalPost.id,{title:'E2E 公开文章',content:'Original body',status:'published',categories:['E2E 分类'],tags:['E2E 标签'],expectedUpdatedAt:currentPost.updatedAt});
    await openAdminSection('分类');const row=page.locator('.taxonomy-row').filter({hasText:'E2E 分类'});await row.getByRole('button',{name:'重命名',exact:true}).click();
    const editing=page.locator('.taxonomy-row').filter({has:page.locator('input')});
    await editing.locator('input').fill('E2E 新分类');await editing.getByRole('button',{name:'保存',exact:true}).click();
    await page.locator('.taxonomy-row').getByText('E2E 新分类',{exact:true}).waitFor();
    assert.equal((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public')).categories[0].name,'E2E 新分类');
    await reader.goto(base+'/user');const categoryMenu=reader.locator('.term-filter-bar details.term-group').filter({hasText:'分类'});await categoryMenu.locator('summary').click();await categoryMenu.getByRole('link').filter({hasText:'E2E 新分类'}).click();
    await reader.locator('.post-card').filter({hasText:'E2E 公开文章'}).waitFor();assert.ok(reader.url().includes('category='));
  });
  await step('reader navigation and taxonomy load beyond the first API page',async()=>{
    for(let i=0;i<101;i++){
      const suffix=String(i).padStart(3,'0');
      await api(owner,'POST','/api/v1/content/pages',{title:'E2E 导航页 '+suffix,slug:'e2e-nav-'+suffix,content:'Navigation fixture',status:'published'},201);
      await api(owner,'POST','/api/v1/content/posts',{title:'E2E Filter fixture '+suffix,slug:'e2e-filter-'+suffix,content:'Filter fixture',status:'published',categories:['E2E filter '+suffix]},201);
    }
    const pageTail=(await api(publicCtx,'GET','/api/v1/content/pages?page=6&pageSize=20')).items[0];assert.ok(pageTail,'fixture should reach a sixth page');
    const termTail=(await api(publicCtx,'GET','/api/v1/content/terms?kind=category&page=2&pageSize=24')).items[0];assert.ok(termTail,'fixture should reach a second category page');
    await reader.goto(base+'/user');
    const pagesMenu=reader.locator('.site-pages');await pagesMenu.locator('summary').click();
    const pageLinks=pagesMenu.locator('.site-pages-panel a');await pageLinks.nth(19).waitFor();
    assert.equal(await pageLinks.count(),20,'initial page navigation should stay bounded');
    const pageLink=pageLinks.nth(100);
    assert.equal(await pageLink.count(),0,'the page from API page six should not be eagerly loaded');
    const morePages=pagesMenu.locator('.site-pages-panel button');await morePages.waitFor();
    for(let batch=0;batch<10;batch++){
      if(await pageLink.count())break;
      if(!(await morePages.count()))break;
      const previousCount=await pageLinks.count();
      await morePages.click();
      await pageLinks.nth(previousCount).waitFor();
    }
    await pageLink.waitFor();assert.equal(await pageLink.getAttribute('href'),`/page/${pageTail.slug}`,'the API page-six result should be the 101st navigation entry');await pageLink.click();
    await reader.locator('.article-body').filter({hasText:'Navigation fixture'}).waitFor();

    await reader.goto(base+'/user');
    const termBar=reader.locator('.term-filter-bar');const categoryMenu=termBar.locator('details.term-group').filter({hasText:'分类'});
    await categoryMenu.locator('summary').click();
    const categoryLinks=categoryMenu.locator('.term-group-options a');await categoryLinks.nth(23).waitFor();
    const termLink=categoryLinks.nth(24);
    assert.equal(await termLink.count(),0,'the category from API page two should not be eagerly loaded');
    const moreTerms=termBar.locator('.term-load-control button');await moreTerms.waitFor();
    for(let batch=0;batch<10;batch++){
      if(await termLink.count())break;
      if(!(await moreTerms.count()))break;
      const previousCount=await categoryLinks.count();
      await moreTerms.click();
      await categoryLinks.nth(previousCount).waitFor();
    }
    await termLink.waitFor();assert.equal(await termLink.getAttribute('href'),`/user?category=${encodeURIComponent(termTail.slug)}`,'the API page-two result should be the 25th category entry');await termLink.click();
    await reader.waitForFunction(slug=>new URL(location.href).searchParams.get('category')===slug,termTail.slug);
    await reader.locator('.post-card').first().waitFor();
  });
  await step('keyboard focus remains visible across reader search controls',async()=>{
    await reader.goto(base+'/user');const searchInput=reader.getByLabel('搜索文章',{exact:true});await searchInput.waitFor();
    let searchFocused=false;
    for(let tab=0;tab<40;tab++){
      if(await searchInput.evaluate(el=>el===document.activeElement)){searchFocused=true;break;}
      await reader.keyboard.press('Tab');
    }
    assert.ok(searchFocused,'Tab navigation should reach the reader search field');
    const inputFocus=await searchInput.evaluate(el=>({matches:el.matches(':focus-visible'),style:getComputedStyle(el).outlineStyle,width:getComputedStyle(el).outlineWidth}));
    assert.deepEqual(inputFocus,{matches:true,style:'solid',width:'2px'});
    await reader.keyboard.press('Tab');
    const searchButton=reader.getByRole('button',{name:'搜索',exact:true});
    assert.ok(await searchButton.evaluate(el=>el===document.activeElement),'Tab should move from search to its submit button');
    assert.equal(await searchButton.evaluate(el=>getComputedStyle(el).outlineStyle),'solid');
    await reader.emulateMedia({forcedColors:'active'});
    const highContrastFocus=await searchButton.evaluate(el=>({matches:el.matches(':focus-visible'),style:getComputedStyle(el).outlineStyle,width:getComputedStyle(el).outlineWidth}));
    assert.deepEqual(highContrastFocus,{matches:true,style:'solid',width:'2px'});
    await reader.keyboard.press('Shift+Tab');
    const highContrastInputFocus=await searchInput.evaluate(el=>({matches:el.matches(':focus-visible'),style:getComputedStyle(el).outlineStyle,width:getComputedStyle(el).outlineWidth}));
    assert.deepEqual(highContrastInputFocus,{matches:true,style:'solid',width:'2px'});
    await reader.keyboard.press('Tab');
    await reader.emulateMedia({forcedColors:'none'});
  });
  await step('search, public pagination and feed privacy',async()=>{
    for(let i=0;i<3;i++)await api(owner,'POST','/api/v1/content/posts',{title:'E2E Search '+i,slug:'e2e-search-'+i,content:'needle-search-body',status:'published'},201);
    await api(owner,'POST','/api/v1/content/posts',{title:'E2E Future',slug:'e2e-future',content:'Future secret',status:'published',publishAt:new Date(Date.now()+86400000).toISOString()},201);
    await page.goto(base+'/setting');await page.getByLabel('每页文章数',{exact:true}).fill('2');await page.getByRole('button',{name:'保存阅读与评论设置',exact:true}).click();await page.getByText('阅读与评论设置已保存',{exact:true}).waitFor();
    await reader.goto(base+'/user');const searchInput=reader.getByLabel('搜索文章',{exact:true});assert.equal(await searchInput.getAttribute('maxlength'),'200');await searchInput.fill('needle-search-body');await reader.getByRole('button',{name:'搜索',exact:true}).click();
    await reader.waitForFunction(()=>document.querySelectorAll('.post-card').length===2);await reader.getByRole('button',{name:'下一页',exact:true}).click();await reader.waitForFunction(()=>document.querySelectorAll('.post-card').length===1);
    await reader.goBack();await reader.waitForFunction(()=>!new URL(location.href).searchParams.has('page')&&document.querySelectorAll('.post-card').length===2);
    await reader.goForward();await reader.waitForFunction(()=>new URL(location.href).searchParams.get('page')==='2'&&document.querySelectorAll('.post-card').length===1);
    const staleSearchRoute=/\/api\/v1\/content\/posts\?.*q=stale-search-query/;
    let releaseStaleSearch,signalStaleSearch;
    const staleSearchStarted=new Promise(resolve=>{signalStaleSearch=resolve});
    const holdStaleSearch=new Promise(resolve=>{releaseStaleSearch=resolve});
    await reader.route(staleSearchRoute,async route=>{const response=await route.fetch();signalStaleSearch();await holdStaleSearch;await route.fulfill({response});},{times:1});
    try{
      await searchInput.fill('stale-search-query');await reader.getByRole('button',{name:'搜索',exact:true}).click();await staleSearchStarted;
      await searchInput.fill('needle-search-body');await reader.getByRole('button',{name:'搜索',exact:true}).click();
      await reader.waitForFunction(()=>new URL(location.href).searchParams.get('q')==='needle-search-body'&&document.querySelectorAll('.post-card').length===2);
      releaseStaleSearch();await reader.waitForLoadState('networkidle');
      assert.equal(await reader.locator('.post-card').count(),2,'an older search response must not replace the latest results');
      assert.equal(await reader.locator('.empty-error').count(),0);
    }finally{
      releaseStaleSearch();await reader.unroute(staleSearchRoute);
    }
    const failedPageCorrectionRoute=/\/api\/v1\/content\/posts\?.*page=2&pageSize=2.*q=needle-search-body/;
    await reader.route(failedPageCorrectionRoute,route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'test_failure',message:'temporary page correction failure'}})}),{times:1});
    try{
      await reader.goto(base+'/user?page=99&q=needle-search-body');
      await reader.locator('.empty-error').getByText('temporary page correction failure',{exact:true}).waitFor();
      assert.equal(new URL(reader.url()).searchParams.get('page'),'99','a failed corrected-page read must not commit the page or URL');
    }finally{
      await reader.unroute(failedPageCorrectionRoute);
    }
    await reader.reload();
    await reader.waitForFunction(()=>new URL(location.href).searchParams.get('page')==='2'&&document.querySelectorAll('.post-card').length===1);
    for(const route of ['/feed.xml','/sitemap.xml']){const text=await(await fetch(base+route)).text();assert.ok(text.includes('e2e-public'));assert.ok(!text.includes('e2e-draft'));assert.ok(!text.includes('e2e-future'));}
    assert.ok((await(await fetch(base+'/robots.txt')).text()).includes('Disallow: /admin'));
  });
  const gif=Buffer.from('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7','base64');
  await step('media upload, insertion and image rendering',async()=>{
    await openAdminSection('媒体');await page.locator('#media').scrollIntoViewIfNeeded();const uploadInput=page.locator('#media input[type=file]');
    const positionedBeforeUpload=await page.evaluate(()=>{const target=document.querySelector('#media input[type=file]');const controls=Array.from(document.querySelectorAll('a[href],area[href],button:not([disabled]),input:not([type="hidden"]):not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])')).filter(el=>el.tabIndex>=0&&el.getClientRects().length>0);const index=controls.indexOf(target);if(index<1)return false;controls[index-1].focus();return true;});
    assert.ok(positionedBeforeUpload,'the media file input should follow a tabbable control');await page.keyboard.press('Tab');assert.ok(await uploadInput.evaluate(el=>el===document.activeElement),'Tab should reach the media file input');
    const uploadFocus=await page.locator('#media .media-upload').evaluate(el=>({containsFocus:el.matches(':focus-within'),style:getComputedStyle(el).outlineStyle,width:getComputedStyle(el).outlineWidth}));assert.deepEqual(uploadFocus,{containsFocus:true,style:'solid',width:'2px'});
    await page.emulateMedia({forcedColors:'active'});const forcedUploadFocus=await page.locator('#media .media-upload').evaluate(el=>({containsFocus:el.matches(':focus-within'),style:getComputedStyle(el).outlineStyle,width:getComputedStyle(el).outlineWidth}));assert.deepEqual(forcedUploadFocus,{containsFocus:true,style:'solid',width:'2px'});await page.emulateMedia({forcedColors:'none'});
    await uploadInput.setInputFiles({name:'e2e.gif',mimeType:'image/gif',buffer:gif});
    await page.waitForFunction(()=>Array.from(document.querySelectorAll('#media .media-card img')).some(i=>i.complete&&i.naturalWidth>0));
    uploaded=(await api(owner,'GET','/api/v1/media')).items[0];
    await openAdminSection('文章');await page.locator('#posts').getByRole('button',{name:'+ 写文章',exact:true}).click();
    const postContent=page.getByPlaceholder('开始写作，支持 Markdown…');await postContent.fill('Before\nAfter');
    await postContent.evaluate(el=>el.setSelectionRange(7,7));
    await page.locator('#post-editor').getByRole('button',{name:'插入图片',exact:true}).click();
    await page.locator('.media-picker-dialog .media-picker-item').filter({hasText:'e2e.gif'}).click();
    assert.equal(await postContent.inputValue(),'Before\n\n![图片说明]('+uploaded.url+')\n\nAfter');
    assert.equal(await postContent.evaluate(el=>el.value.slice(el.selectionStart,el.selectionEnd)),'图片说明');
    await page.locator('#post-editor').getByRole('button',{name:'看预览',exact:true}).click();
    const postPreview=page.locator('#post-editor .editor-preview img');await postPreview.waitFor();
    assert.equal(await postPreview.getAttribute('src'),uploaded.url);assert.equal(await postPreview.getAttribute('alt'),'图片说明');
    await page.locator('#posts').getByRole('button',{name:'关闭编辑器',exact:true}).click();
    await openAdminSection('页面');await page.locator('#pages').getByRole('button',{name:'+ 新建页面',exact:true}).click();
    const pageContent=page.getByPlaceholder('页面内容，支持 Markdown…');await pageContent.fill('Before\nDELETE\nAfter');
    await pageContent.evaluate(el=>el.setSelectionRange(7,13));
    await page.locator('#page-editor').getByRole('button',{name:'插入图片',exact:true}).click();
    await page.locator('.media-picker-dialog .media-picker-item').filter({hasText:'e2e.gif'}).click();
    assert.equal(await pageContent.inputValue(),'Before\n\n![图片说明]('+uploaded.url+')\n\nAfter');
    await page.locator('#page-editor').getByRole('button',{name:'看预览',exact:true}).click();
    const pagePreview=page.locator('#page-editor .editor-preview img');await pagePreview.waitFor();
    assert.equal(await pagePreview.getAttribute('src'),uploaded.url);
    const expectedPageHTML=await page.locator('#page-editor .editor-preview').evaluate(el=>el.innerHTML);
    await page.locator('#page-editor .editor-title').fill('E2E Markdown Image');await page.locator('#page-editor select').selectOption('published');
    await page.locator('#page-editor').getByRole('button',{name:'保存页面',exact:true}).click();
    await page.locator('#pages .admin-post-row').filter({hasText:'E2E Markdown Image'}).waitFor();
    await reader.goto(base+'/page/e2e-markdown-image');await reader.locator('.article-body img').waitFor();
    assert.equal(await reader.locator('.article-body').innerHTML(),expectedPageHTML,'page preview should match the reader renderer');
    const imagePage=(await api(owner,'GET','/api/v1/content/pages?includeDraft=true&page=1&pageSize=100')).items.find(item=>item.slug==='e2e-markdown-image');
    assert.ok(imagePage);await api(owner,'DELETE','/api/v1/content/pages/'+imagePage.id,undefined,204);
    const response=await publicCtx.request.get(base+uploaded.url);assert.equal(response.status(),200);assert.deepEqual(await response.body(),gif);
  });
  await step('mobile admin navigation and content fit',async()=>{
    const sections=[['概览','overview'],['文章','posts'],['页面','pages'],['评论','comments'],['媒体','media'],['分类','taxonomy'],['通知','mail']];
    for(const width of [390,320]){
      await page.setViewportSize({width,height:844});
      for(const [name,id] of sections){
        await openAdminSection(name);
        const section=page.locator('#'+id);
        await section.waitFor({state:'visible'});
        assert.equal(await page.locator('.command-nav').getByRole('link',{name,exact:true}).getAttribute('aria-current'),'page');
        assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'admin '+name+' overflows at '+width+'px');
      }
      if(width===390)await page.screenshot({path:path.join(artifacts,'mobile-admin.png'),fullPage:false});
    }
    await page.setViewportSize({width:1280,height:800});
  });
  await step('protected post hides body and supports password retry',async()=>{
    await api(owner,'POST','/api/v1/content/posts',{title:'E2E Protected',slug:'e2e-protected',content:'Protected secret body',status:'published',passphrase:'read-password-123',customFields:{secret:'Protected custom value'}},201);
    const locked=await api(publicCtx,'GET','/api/v1/content/posts/e2e-protected');assert.ok(locked.locked);assert.ok(!locked.content);assert.equal('customFields' in locked,false);
    assert.ok(!(await(await fetch(base+'/user/e2e-protected')).text()).includes('Protected secret body'));
    await reader.goto(base+'/user/e2e-protected');await reader.getByLabel('阅读口令',{exact:true}).fill('wrong-password');await reader.getByRole('button',{name:'查看正文',exact:true}).click();
    await reader.locator('.form-error').waitFor();await reader.getByLabel('阅读口令',{exact:true}).fill('read-password-123');await reader.getByRole('button',{name:'查看正文',exact:true}).click();
    await reader.locator('.article-body').filter({hasText:'Protected secret body'}).waitFor();
    await reader.locator('.post-custom-fields').getByText('Protected custom value',{exact:true}).waitFor();
  });
  await step('reject disguised image uploads',async()=>{
    const token=(await owner.cookies(base)).find(c=>c.name==='app_csrf').value;
    const response=await owner.request.post(base+'/api/v1/media',{headers:{'X-CSRF-Token':token},multipart:{file:{name:'fake.png',mimeType:'image/png',buffer:Buffer.from('<script>bad image</script>')}}});
    assert.equal(response.status(),415);assert.equal((await response.json()).error.code,'unsupported_media');
  });
  await step('anonymous comment, moderation and threaded reply',async()=>{
    await api(publicCtx,'POST','/api/v1/content/posts/e2e-public/comments',{authorName:'E2E 欺骗链接',authorUrl:'https://trusted.example@attacker.example',content:'Should be rejected'},422);
    await reader.goto(base+'/user/e2e-public');await reader.getByLabel('称呼',{exact:true}).fill('E2E 访客');await reader.getByLabel('评论',{exact:true}).fill('E2E 待审核评论');await reader.getByRole('button',{name:'发表评论',exact:true}).click();await reader.locator('.form-success').waitFor();
    assert.equal((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public/comments')).total,0);
    await openAdminSection('评论');const comments=page.locator('#comments');const row=comments.locator('.admin-comment-row').filter({hasText:'E2E 待审核评论'});await row.getByRole('button',{name:'通过',exact:true}).click();await row.waitFor({state:'detached'});
    const commentsRoute=/\/api\/v1\/content\/posts\/e2e-public\/comments\?/;
    await reader.route(commentsRoute,async route=>{const response=await route.fetch();const result=await response.json();const visitor=result.items.find(item=>item.authorName==='E2E 访客');if(visitor)visitor.authorUrl='https://trusted.example@attacker.example';await route.fulfill({response,body:JSON.stringify(result)});},{times:1});
    await reader.reload();const comment=reader.locator('.comment-card').filter({hasText:'E2E 待审核评论'});await comment.getByRole('button',{name:'回复',exact:true}).waitFor();
    const visitorName=comment.locator('.comment-person');assert.equal(await visitorName.locator('a').count(),0,'legacy URLs with userinfo must not be clickable');assert.equal(await visitorName.locator('strong').innerText(),'E2E 访客');
    await reader.unroute(commentsRoute);await comment.getByRole('button',{name:'回复',exact:true}).click();
    await reader.getByLabel('称呼',{exact:true}).fill('E2E 回复者');await reader.getByLabel('评论',{exact:true}).fill('E2E 线程回复');await reader.getByRole('button',{name:'发表回复',exact:true}).click();await reader.locator('.form-success').waitFor();
    await page.reload();await page.locator('#comments .admin-comment-row').filter({hasText:'E2E 线程回复'}).getByRole('button',{name:'通过',exact:true}).click();
    await reader.reload();await reader.locator('.comment-card[data-depth="1"]').filter({hasText:'E2E 线程回复'}).waitFor();
  });
  await step('comment closure and CSRF enforcement',async()=>{
    await page.goto(base+'/setting');await page.getByLabel('开放评论',{exact:true}).uncheck();await page.getByRole('button',{name:'保存阅读与评论设置',exact:true}).click();await page.getByText('阅读与评论设置已保存',{exact:true}).waitFor();
    await reader.goto(base+'/user/e2e-public');await reader.getByText('评论已关闭。',{exact:true}).waitFor();assert.equal(await reader.locator('.comment-form').count(),0);
    await api(owner,'POST','/api/v1/content/posts',{title:'CSRF blocked',content:'blocked',status:'draft'},403,false);
    await page.getByLabel('开放评论',{exact:true}).check();await page.getByRole('button',{name:'保存阅读与评论设置',exact:true}).click();
  });
  await step('notification history pagination commits after its response',async()=>{
    for(let i=0;i<21;i++)await api(owner,'POST','/api/v1/content/posts/e2e-public/comments',{authorName:'E2E 通知分页',authorEmail:'mail-page@example.com',content:'Notification fixture '+i},202);
    await openAdminSection('通知');const notifications=page.locator('#mail');const pager=notifications.locator('.pagination-controls');await pager.waitFor();
    const mailRoute=/\/api\/v1\/mail\?page=2&pageSize=20/;let releaseMailPage,signalMailPage,secondMailPage;
    const mailPageStarted=new Promise(resolve=>{signalMailPage=resolve});
    const holdMailPage=new Promise(resolve=>{releaseMailPage=resolve});
    await page.route(mailRoute,async route=>{const response=await route.fetch();secondMailPage=await response.json();signalMailPage();await holdMailPage;await route.fulfill({response});},{times:1});
    try{
      await pager.getByRole('button',{name:'下一页',exact:true}).click();await mailPageStarted;
      assert.equal(await pager.locator('span').innerText(),'第 1 / 2 页');
      assert.ok(await pager.getByRole('button',{name:'下一页',exact:true}).isDisabled());
      releaseMailPage();await pager.getByText('第 2 / 2 页').waitFor();
      assert.equal(await notifications.locator('.mail-body').first().innerText(),secondMailPage.items[0].body);
    }finally{
      releaseMailPage();await page.unroute(mailRoute);
    }
  });
  await step('failed comment pagination can retry the same page',async()=>{
    const reading=await api(owner,'GET','/api/v1/settings/reading');
    await api(owner,'PUT','/api/v1/settings/reading',{...reading,commentsRequireModeration:false});
    for(let i=0;i<99;i++)await api(owner,'POST','/api/v1/content/posts/e2e-public/comments',{authorName:'E2E 评论分页',authorEmail:'comment-page@example.com',content:'Comment fixture '+i},201);
    await api(owner,'PUT','/api/v1/settings/reading',reading);
    await reader.goto(base+'/user/e2e-public');await reader.getByText('已显示 100 / 101').waitFor();
    const commentPageRoute=/\/api\/v1\/content\/posts\/e2e-public\/comments\?page=2&pageSize=100&cursor=/;
    await reader.route(commentPageRoute,route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'test_failure',message:'temporary comment read failure'}})}),{times:1});
    const more=reader.getByRole('button',{name:'加载更多评论',exact:true});await more.click();
    await reader.locator('.comment-section .form-error').getByText('temporary comment read failure',{exact:true}).waitFor();
    assert.equal(await reader.locator('.comment-card').count(),100,'a failed append must keep the current comments visible');
    await reader.unroute(commentPageRoute);
    const retryRequest=reader.waitForRequest(request=>request.url().includes('/api/v1/content/posts/e2e-public/comments?page=2&pageSize=100&cursor='));
    await reader.getByRole('button',{name:'重试加载评论',exact:true}).click();
    const retry=await retryRequest;
    assert.equal(new URL(retry.url()).searchParams.get('page'),'2','retry must request the same page after a failed load');
    assert.ok(new URL(retry.url()).searchParams.get('cursor'),'retry must preserve the cursor for the failed page');
    const retryResponse=await retry.response();assert.ok(retryResponse?.ok(),`comment retry failed: ${retryResponse?.status()}`);
    const retryData=await retryResponse.json();assert.equal(retryData.page,2);assert.equal(retryData.items.length,1);assert.equal(retryData.total,101);
    await reader.waitForFunction(()=>document.querySelectorAll('.comment-section .comment-card').length===101&&!document.querySelector('.comment-section .pagination'));
    assert.equal(await reader.locator('.comment-card').count(),101);
    const firstPageRoute=/\/api\/v1\/content\/posts\/e2e-public\/comments\?page=1&pageSize=100/;
    await reader.route(firstPageRoute,route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'test_failure',message:'temporary first page failure'}})}),{times:1});
    await reader.reload();
    await reader.locator('.comment-section .form-error').getByText('temporary first page failure',{exact:true}).waitFor();
    assert.equal(await reader.locator('.comment-card').count(),0,'an initial read failure must expose its retry state without stale comments');
    await reader.unroute(firstPageRoute);
    const firstPageRetryRequest=reader.waitForRequest(request=>request.url().includes('/api/v1/content/posts/e2e-public/comments?page=1&pageSize=100'));
    await reader.getByRole('button',{name:'重试加载评论',exact:true}).click();
    const firstPageRetry=await firstPageRetryRequest;
    assert.equal(new URL(firstPageRetry.url()).searchParams.get('page'),'1','initial retry must request the first page');
    await reader.waitForFunction(()=>document.querySelectorAll('.comment-section .comment-card').length===100&&document.querySelector('.comment-section .pagination'));
  });
  await step('editor/moderator permissions and immediate user disable',async()=>{
    await page.goto(base+'/setting');
    for(const role of ['editor','moderator']){
      const form=page.locator('.user-create-form');await form.getByLabel('用户名',{exact:true}).fill(role);await form.getByLabel('邮箱',{exact:true}).fill(role+'@example.com');await form.getByLabel('初始密码',{exact:true}).fill('password-staff-123');await form.locator('select').selectOption(role);await form.getByRole('button',{name:'创建用户',exact:true}).click();await page.locator('.user-row').filter({hasText:role+'@example.com'}).waitFor();
    }
    const editor=await context();const ep=await login(editor,'editor','password-staff-123');assert.equal(await ep.getByRole('link',{name:'备份',exact:true}).count(),0);
    await api(editor,'GET','/api/v1/backup',undefined,403);await api(editor,'GET','/api/v1/settings/users',undefined,403);
    await api(editor,'POST','/api/v1/content/posts',{title:'Staff draft',slug:'staff-draft',content:'Editor allowed',status:'draft'},201);
    const moderator=await context();const mp=await login(moderator,'moderator','password-staff-123');assert.equal(await mp.getByRole('button',{name:'+ 写文章',exact:true}).count(),0);
    await api(moderator,'POST','/api/v1/content/posts',{title:'Blocked',content:'Blocked',status:'draft'},403);await api(moderator,'GET','/api/v1/content/comments');
    await mp.locator('button.avatar').click();await mp.getByRole('button',{name:'登录管理端',exact:true}).waitFor();await api(moderator,'GET','/api/v1/auth/me',undefined,401);
    const row=page.locator('.user-row').filter({hasText:'editor@example.com'});await row.getByLabel('启用',{exact:true}).uncheck();
    await page.getByText('用户信息已保存',{exact:true}).waitFor();
    await api(editor,'GET','/api/v1/auth/me',undefined,401);
  });
  await step('admin pagination beyond 100 and audit pagination',async()=>{
    for(let i=0;i<101;i++)await api(owner,'POST','/api/v1/content/posts',{title:'Paging fixture '+String(i).padStart(3,'0'),slug:'paging-'+i,content:'paging',status:'draft'},201);
    await openAdminSection('文章');const posts=page.locator('#posts');await posts.getByRole('button',{name:'草稿',exact:true}).click();
    await page.waitForFunction(()=>{const rows=[...document.querySelectorAll('#posts .admin-post-row')];return rows.length>0&&rows.every(row=>row.querySelector('.post-status')?.classList.contains('draft'));});
    const draftPager=posts.locator('.pagination');const draftSummary=await draftPager.locator('span').innerText();const draftPages=draftSummary.match(/^1 \/ (\d+) · 共 (\d+) 篇$/);assert.ok(draftPages&&Number(draftPages[1])>=2&&Number(draftPages[2])>=101,'draft filter should report its own cross-page total');
    await draftPager.getByRole('button',{name:'下一页',exact:true}).click();await draftPager.locator('span').filter({hasText:/^2 \/ \d+/}).waitFor();
    await page.waitForFunction(()=>{const rows=[...document.querySelectorAll('#posts .admin-post-row')];return rows.length>0&&rows.every(row=>row.querySelector('.post-status')?.classList.contains('draft'));});
    await posts.getByRole('button',{name:'已发布',exact:true}).click();await page.waitForFunction(()=>{const rows=[...document.querySelectorAll('#posts .admin-post-row')];return rows.length>0&&rows.every(row=>row.querySelector('.post-status')?.classList.contains('published'));});
    await posts.getByRole('button',{name:'全部',exact:true}).click();const postsPager=posts.locator('.pagination');await postsPager.getByRole('button',{name:'下一页',exact:true}).click();await postsPager.locator('span').filter({hasText:/^2 \/ \d+/}).waitFor();
    await page.waitForFunction(()=>{const count=document.querySelectorAll('#posts .admin-post-row').length;return count>0&&count<=100;});
    await postsPager.getByRole('button',{name:'上一页',exact:true}).click();await postsPager.locator('span').filter({hasText:/^1 \/ \d+/}).waitFor();
    const shrinkingPostsRoute=/\/api\/v1\/content\/posts\?.*page=2&pageSize=100/;
    await page.route(shrinkingPostsRoute,async route=>{const response=await route.fetch();const result=await response.json();result.items=[];result.total=100;await route.fulfill({response,body:JSON.stringify(result)});},{times:1});
    const repairedPostsPage=page.waitForRequest(request=>/\/api\/v1\/content\/posts\?.*page=1&pageSize=100/.test(request.url()));
    await postsPager.getByRole('button',{name:'下一页',exact:true}).click();await repairedPostsPage;
    await postsPager.locator('span').filter({hasText:/^1 \/ \d+/}).waitFor();
    await page.waitForFunction(()=>{const rows=document.querySelectorAll('#posts .admin-post-row');return rows.length>0&&rows.length<=100;});
    await page.goto(base+'/setting');const auditPager=page.locator('.settings-audit .pagination');
    const auditRoute=/\/api\/v1\/audit\?page=2&pageSize=20/;let releaseAuditPage,signalAuditPage,auditPageTwo;
    const auditPageStarted=new Promise(resolve=>{signalAuditPage=resolve});
    const holdAuditPage=new Promise(resolve=>{releaseAuditPage=resolve});
    await page.route(auditRoute,async route=>{const response=await route.fetch();auditPageTwo=await response.json();signalAuditPage();await holdAuditPage;await route.fulfill({response});},{times:1});
    try{
      await auditPager.getByRole('button',{name:'下一页',exact:true}).click();await auditPageStarted;
      assert.ok((await auditPager.locator('span').innerText()).startsWith('1 /'));
      assert.ok(await auditPager.getByRole('button',{name:'下一页',exact:true}).isDisabled());
      releaseAuditPage();await auditPager.getByText(/2 \//).waitFor();
      await page.locator('.audit-row').first().filter({hasText:auditPageTwo.items[0].resourceId}).waitFor();
    }finally{
      releaseAuditPage();await page.unroute(auditRoute);
    }
    await auditPager.getByRole('button',{name:'上一页',exact:true}).click();await auditPager.locator('span').filter({hasText:/^1 \/ \d+/}).waitFor();
    const shrinkingAuditRoute=/\/api\/v1\/audit\?page=2&pageSize=20/;
    await page.route(shrinkingAuditRoute,async route=>{const response=await route.fetch();const result=await response.json();result.items=[];result.total=20;await route.fulfill({response,body:JSON.stringify(result)});},{times:1});
    const repairedAuditPage=page.waitForRequest(request=>request.url().includes('/api/v1/audit?page=1&pageSize=20'));
    await auditPager.getByRole('button',{name:'下一页',exact:true}).click();await repairedAuditPage;
    await auditPager.locator('span').filter({hasText:/^1 \/ \d+/}).waitFor();
    await page.locator('.settings-audit .audit-row').first().waitFor();
  });
  await step('dashboard keeps committed filters and ignores stale loads after logout',async()=>{
    await openAdminSection('概览');await page.locator('.stats-grid').waitFor();
    await openAdminSection('文章');
    await page.locator('#posts .section-count').waitFor();
    const postsRoute=/\/api\/v1\/content\/posts\?/;
    await page.route(postsRoute,route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:{code:'test_failure',message:'temporary read failure'}})}),{times:1});
    await page.locator('#posts').getByRole('button',{name:'草稿',exact:true}).click();
    await page.getByText('temporary read failure',{exact:true}).waitFor();
    assert.equal(await page.locator('#posts .term-filter.active').innerText(),'全部','failed filter loads must keep the visible list filter');
    await page.unroute(postsRoute);

    const authRoute='**/api/v1/auth/me';let releaseAuthMe,signalAuthMe;
    const authMeStarted=new Promise(resolve=>{signalAuthMe=resolve});
    const holdAuthMe=new Promise(resolve=>{releaseAuthMe=resolve});
    await page.route(authRoute,async route=>{const response=await route.fetch();signalAuthMe();await holdAuthMe;await route.fulfill({response});});
    try{
      await page.locator('#posts').getByRole('button',{name:'已发布',exact:true}).click();await authMeStarted;
      await page.locator('button.avatar').click();await page.getByRole('button',{name:'登录管理端',exact:true}).waitFor();
      const authMeResponse=page.waitForResponse(response=>response.url().includes('/api/v1/auth/me'));
      releaseAuthMe();await authMeResponse;await page.waitForLoadState('networkidle');
      assert.equal(await page.locator('.stats-grid').count(),0,'a stale dashboard response must not restore the signed-in view');
      assert.equal(await page.locator('.login-panel').count(),1);
    }finally{
      releaseAuthMe();await page.unroute(authRoute);
    }
    await page.close();page=await login(owner,'owner','password-owner-123');page.on('dialog',dialog=>dialog.accept());
  });
  await step('owner password change revokes other sessions',async()=>{
    const other=await context();await login(other,'owner','password-owner-123');
    await page.goto(base+'/setting');await page.getByLabel('当前密码',{exact:true}).fill('password-owner-123');await page.getByLabel('新密码',{exact:true}).fill('password-owner-456');await page.getByRole('button',{name:'保存账户信息',exact:true}).click();await page.getByText('账户信息已保存',{exact:true}).waitFor();
    await api(owner,'GET','/api/v1/auth/me');await api(other,'GET','/api/v1/auth/me',undefined,401);
  });
  await step('backup download and missing media failure',async()=>{
    await page.addInitScript(()=>Object.defineProperty(window,'showSaveFilePicker',{configurable:true,value:async options=>{
      window.__e2eBackup={name:options.suggestedName,chunks:[],base64:''};
      return {createWritable:async()=>new WritableStream({write(chunk){window.__e2eBackup.chunks.push(Array.from(chunk));},close(){
        const length=window.__e2eBackup.chunks.reduce((total,chunk)=>total+chunk.length,0);const bytes=new Uint8Array(length);let offset=0;
        for(const chunk of window.__e2eBackup.chunks){bytes.set(chunk,offset);offset+=chunk.length;}
        let binary='';for(let start=0;start<bytes.length;start+=0x8000)binary+=String.fromCharCode(...bytes.subarray(start,start+0x8000));window.__e2eBackup.base64=btoa(binary);
      }})};
    }}));
    await page.goto(base+'/admin');await page.getByRole('button',{name:'备份',exact:true}).click();await page.getByText('备份已保存',{exact:true}).waitFor();
    const streamedArchive=await page.evaluate(()=>window.__e2eBackup);assert.equal(streamedArchive.name,'typecho-backup.zip');
    backupPath=path.join(scratch,'backup.zip');await fs.writeFile(backupPath,Buffer.from(streamedArchive.base64,'base64'));assert.ok((await fs.stat(backupPath)).size>0);
    const fallbackPage=await owner.newPage();await fallbackPage.addInitScript(()=>Object.defineProperty(window,'showSaveFilePicker',{configurable:true,value:undefined}));
    await fallbackPage.goto(base+'/admin');const downloading=fallbackPage.waitForEvent('download');await fallbackPage.getByRole('link',{name:'备份',exact:true}).click();
    const fallbackDownload=await downloading;assert.equal(await fallbackDownload.failure(),null);const fallbackPath=path.join(scratch,'fallback-backup.zip');
    await fallbackDownload.saveAs(fallbackPath);assert.ok((await fs.stat(fallbackPath)).size>0);await fallbackPage.close();
    const mediaRoot=path.join(dataDir,'uploads');const file=(await fs.readdir(mediaRoot)).find(n=>!n.startsWith('.'));await fs.rename(path.join(mediaRoot,file),path.join(scratch,'held-media'));
    try {await api(owner,'GET','/api/v1/backup',undefined,500);}finally{await fs.rename(path.join(scratch,'held-media'),path.join(mediaRoot,file));}
  });
  await step('restart persists content, media and sessions',async()=>{
    // Persist cookies across server ports, which are not part of cookie scope.
    await stop();await start(dataDir);await page.goto(base+'/admin');await page.locator('.stats-grid').waitFor();await api(owner,'GET','/api/v1/auth/me');
    assert.equal((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public')).title,'E2E 公开文章');
    assert.equal((await publicCtx.request.get(base+uploaded.url)).status(),200);
  });
  await step('restore ZIP into empty directories and verify service',async()=>{
    await stop();const recovery=path.join(scratch,'recovery');
    execFileSync('powershell.exe',['-NoProfile','-Command','Expand-Archive -LiteralPath $env:E2E_ZIP -DestinationPath $env:E2E_RECOVERY'],{env:{...process.env,E2E_ZIP:backupPath,E2E_RECOVERY:recovery},windowsHide:true});
    await start(recovery,path.join(recovery,'media'));await page.goto(base+'/admin');await page.locator('.stats-grid').waitFor();await api(owner,'GET','/api/v1/auth/me');
    const restoredPost=await api(publicCtx,'GET','/api/v1/content/posts/e2e-public');assert.equal(restoredPost.title,'E2E 公开文章');assert.equal(restoredPost.content,'Original body');
    assert.equal((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public/comments')).total,101);
    const response=await publicCtx.request.get(base+uploaded.url);assert.equal(response.status(),200);assert.deepEqual(await response.body(),gif);
    await api(publicCtx,'GET','/api/v1/content/posts/e2e-draft',undefined,404);
  });
  await step('delete post, media, taxonomy and staff through UI',async()=>{
    await openAdminSection('文章');const row=postRow('E2E Search 2');await row.getByRole('button',{name:'删除',exact:true}).click();await row.waitFor({state:'detached'});await api(publicCtx,'GET','/api/v1/content/posts/e2e-search-2',undefined,404);
    await openAdminSection('媒体');const asset=page.locator('.media-card').filter({hasText:'e2e.gif'});await asset.getByRole('button',{name:'删除',exact:true}).click();await asset.waitFor({state:'detached'});assert.equal((await publicCtx.request.get(base+uploaded.url)).status(),404);
    await openAdminSection('分类');
    const taxonomy=page.locator('#taxonomy');const term=taxonomy.locator('.taxonomy-row').filter({hasText:'E2E 标签'});await taxonomy.locator('.taxonomy-row').first().waitFor();
    for(let pageNo=1;pageNo<20&&(await term.count())===0;pageNo++){const next=taxonomy.locator('.pagination').getByRole('button',{name:'下一页',exact:true});assert.ok(await next.isEnabled(),'target taxonomy should exist on a later page');await next.click();await page.waitForFunction(expected=>document.querySelector('#taxonomy .pagination span')?.textContent?.startsWith(expected),`${pageNo+1} /`);}
    await term.getByRole('button',{name:'删除',exact:true}).click();await term.waitFor({state:'detached'});assert.equal(((await api(publicCtx,'GET','/api/v1/content/posts/e2e-public')).tags||[]).length,0);
    await page.goto(base+'/setting');const staff=page.locator('.user-row').filter({hasText:'moderator@example.com'});await staff.getByRole('button',{name:'删除',exact:true}).click();await staff.waitFor({state:'detached'});
    assert.ok(!(await api(owner,'GET','/api/v1/settings/users')).items.some(u=>u.username==='moderator'));
  });
  await step('mobile reader and no browser exceptions',async()=>{
    await reader.setViewportSize({width:390,height:844});await reader.goto(base+'/user/e2e-public');await reader.locator('.article-body').waitFor();
    assert.ok(await reader.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'mobile page has horizontal overflow');
    const menuToggle=reader.locator('.nav-toggle');const rail=reader.locator('#reader-rail');const railLink=rail.locator('a').first();const railLastLink=rail.locator('a').last();const railClose=rail.getByRole('button',{name:'关闭目录',exact:true});const tabToRailLink=async()=>{let focused=false;for(let tab=0;tab<20;tab++){await reader.keyboard.press('Tab');if(await railLink.evaluate(el=>el===document.activeElement)){focused=true;break;}}assert.ok(focused,'Tab navigation should reach the mobile reader rail');};
    assert.equal(await rail.evaluate(el=>getComputedStyle(el).display),'none','closed mobile rail should be absent from the tab sequence');
    await menuToggle.click();assert.equal(await menuToggle.getAttribute('aria-expanded'),'true');await railClose.waitFor();await reader.getByRole('dialog',{name:'归档与最新动态'}).waitFor();assert.ok(await railClose.evaluate(el=>el===document.activeElement),'opening the mobile rail should move focus inside it');assert.equal(await reader.locator('.public-bar').evaluate(el=>el.hasAttribute('inert')),true);assert.equal(await reader.locator('.public-content').evaluate(el=>el.hasAttribute('inert')),true);
    await railLastLink.focus();await reader.keyboard.press('Tab');assert.ok(await railClose.evaluate(el=>el===document.activeElement),'Tab from the last rail link should wrap to the close button');await reader.keyboard.press('Shift+Tab');assert.ok(await railLastLink.evaluate(el=>el===document.activeElement),'Shift+Tab from the first rail control should wrap to the last link');
    await reader.keyboard.press('Escape');assert.equal(await menuToggle.getAttribute('aria-expanded'),'false');assert.ok(await menuToggle.evaluate(el=>el===document.activeElement),'Escape should restore focus to the mobile menu toggle');
    await menuToggle.click();await railClose.waitFor();await railClose.click();assert.equal(await menuToggle.getAttribute('aria-expanded'),'false');assert.ok(await menuToggle.evaluate(el=>el===document.activeElement),'the dialog close button should restore focus to its toggle');
    await menuToggle.click();await tabToRailLink();await reader.locator('.rail-backdrop').click({position:{x:2,y:300}});assert.equal(await menuToggle.getAttribute('aria-expanded'),'false');assert.ok(await menuToggle.evaluate(el=>el===document.activeElement),'closing the mobile menu backdrop should restore focus to its toggle');
    await reader.screenshot({path:path.join(artifacts,'mobile-reader.png'),fullPage:true});await page.goto(base+'/admin');await page.locator('.stats-grid').waitFor();await page.screenshot({path:path.join(artifacts,'admin.png'),fullPage:true});assert.deepEqual(errors,[]);
  });
})().catch(async error=>{
  console.error(error);process.exitCode=1;
  if(page)await page.screenshot({path:path.join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});
}).finally(async()=>{
  if(browser)await browser.close();await stop();
  await fs.mkdir(artifacts,{recursive:true});
  await fs.writeFile(path.join(artifacts,'report.json'),JSON.stringify({date:new Date().toISOString(),platform:process.platform,scratch,results,browserErrors:errors},null,2));
  console.log(`${results.filter(r=>r.status==='passed').length}/${results.length} passed; report: ${path.join(artifacts,'report.json')}`);
  if(scratch)await fs.rm(scratch,{recursive:true,force:true,maxRetries:3,retryDelay:100});
});
