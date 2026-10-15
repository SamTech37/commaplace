// End-to-end check for section embeds, block markers, section hover preview
// and [[note# heading autocomplete, against a running dev server.
//
// Manual, not CI: it needs Playwright and a browser, which this repo does not
// vendor. Run against a server started with DEBUG=1 (for /_dev/login):
//
//   NODE_PATH=$(npm root -g) node scripts/ofm-e2e.cjs            # http://localhost:8080
//   BASE=http://localhost:8099 NODE_PATH=$(npm root -g) node scripts/ofm-e2e.cjs
//
// Creates two notes as alice with a per-run suffix, so it can be rerun.
// Screenshots land in $OUT (default: the current directory).
const { chromium } = require('playwright');

const BASE = process.env.BASE || 'http://localhost:8080';
const OUT = process.env.OUT || '.';
const run = Date.now().toString(36);
const fails = [];
const check = (ok, msg) => {
  console.log((ok ? 'PASS ' : 'FAIL ') + msg);
  if (!ok) fails.push(msg);
};

(async () => {
  const browser = await chromium.launch(process.env.CHROMIUM ? { executablePath: process.env.CHROMIUM } : {});
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 900 } })).newPage();
  // Only failures this feature could cause: script exceptions, and errors
  // from our own endpoints. CDN fonts/KaTeX and missing avatars are noise.
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('response', r => {
    if (r.url().startsWith(BASE) && (r.status() >= 500 || (r.status() >= 400 && /\/api\/(preview|wiki)\//.test(r.url())))) {
      errors.push(r.status() + ' ' + r.url());
    }
  });

  await page.goto(BASE + '/_dev/login?as=alice');
  const write = (title, body) => page.evaluate(async ([title, body]) => {
    const f = new FormData();
    f.set('title', title);
    f.set('body_md', body);
    return (await fetch('/write', { method: 'POST', body: f })).url;
  }, [title, body]);

  const guideURL = await write('E2E Guide ' + run,
    '# Intro\nintro text\n## Install\nINSTALL-BODY\n## Usage\nUSAGE-BODY\n### Flags\nFLAGS-BODY\n# Appendix\nAPPENDIX-BODY\n\nA paragraph. ^blk1\n');
  const guide = guideURL.split('/').pop();
  check(guide.startsWith('e2e-guide'), 'create guide note (' + guideURL + ')');
  const readerURL = await write('E2E Reader ' + run,
    `See [[${guide}#Usage]] and [[${guide}#^blk1]].\n\n![[${guide}#Install]]\n\n![[${guide}#Missing]]\n\n![[${guide}#^blk1]]\n`);
  check(readerURL.includes('/alice/e2e-reader'), 'create reader note (' + readerURL + ')');

  // Section embeds and hidden block markers.
  await page.goto(readerURL);
  const embeds = await page.locator('blockquote.embed .embed-body').allInnerTexts();
  check(embeds[0]?.includes('INSTALL-BODY') && !embeds[0]?.includes('USAGE-BODY'), 'heading embed shows only its section');
  check(embeds[1]?.includes('找不到段落') && embeds[1]?.includes('#Missing'), 'unknown heading shows the missing-section line');
  check(embeds[2]?.includes('APPENDIX-BODY') && embeds[2]?.includes('INSTALL-BODY'), 'block embed (block refs off) shows the whole note');
  check(!(await page.locator('.article-body').innerText()).includes('^blk1'), 'block marker is never visible');
  const blockHref = await page.locator(`.article-body p a[href^="/alice/${guide}"]`).nth(1).getAttribute('href');
  check(blockHref === `/alice/${guide}`, 'block link lands on the note (' + blockHref + ')');

  // Hover a heading link: the card is that section.
  await page.locator('a[data-anchor="Usage"]').first().hover();
  const card = page.locator('.link-preview-card .preview-section');
  await card.waitFor({ state: 'visible', timeout: 5000 }).catch(() => {});
  const cardText = await card.innerText().catch(() => '');
  check(cardText.includes('USAGE-BODY') && cardText.includes('FLAGS-BODY') && !cardText.includes('APPENDIX-BODY'),
    'hover on [[note#Usage]] previews the Usage section');
  await page.screenshot({ path: OUT + '/ofm-hover-section.png' });
  await page.mouse.move(5, 5);

  // The preview script now lives in the layout: once per page, everywhere.
  for (const path of [new URL(readerURL).pathname, '/feed', '/alice', '/graph', '/search?q=e2e']) {
    await page.goto(BASE + path);
    check(await page.locator('script[src*="linkpreview.js"]').count() === 1, 'linkpreview.js loaded once on ' + path);
  }

  // Editor: [[note# lists the target's headings; Enter inserts one.
  await page.goto(BASE + '/write');
  await page.locator('.CodeMirror').click();
  await page.keyboard.type(`Title line\n\nSee [[${guide}#us`);
  await page.locator('#ac-popup .ac-item').first().waitFor({ state: 'visible', timeout: 5000 }).catch(() => {});
  const items = await page.locator('#ac-popup .ac-item').allInnerTexts();
  check(items.length === 1 && items[0].includes('Usage'), 'autocomplete lists the matching heading ' + JSON.stringify(items));
  await page.screenshot({ path: OUT + '/ofm-editor-headings.png' });
  await page.keyboard.press('Enter');
  const doc = await page.evaluate(() => document.querySelector('.CodeMirror').CodeMirror.getValue());
  check(doc.includes(`[[${guide}#Usage]]`), 'Enter inserts the heading link');

  // POST /preview (the import preview) renders section embeds.
  const prev = await page.evaluate(async (g) => {
    const r = await fetch('/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'body_md=' + encodeURIComponent(`![[${g}#Usage]]`),
    });
    return r.text();
  }, guide);
  check(prev.includes('USAGE-BODY') && !prev.includes('INSTALL-BODY'), 'POST /preview renders the section embed');

  check(errors.length === 0, 'no script errors or failing feature endpoints ' + JSON.stringify(errors));
  await browser.close();
  console.log(fails.length ? fails.length + ' FAILED' : 'ALL PASSED');
  process.exit(fails.length ? 1 : 0);
})();
