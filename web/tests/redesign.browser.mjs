import assert from 'node:assert/strict'

// 固定公开数据，覆盖所有页面；不连接运行中的后端或数据库。
export const designItems = [
  ['开源模型更新：能力、成本与使用门槛', '模型', '官方博客'],
  ['AI 编程工具，正在改变哪些工作？', '产品', '开发者社区'],
  ['从论文到应用：多模态模型的新进展', '论文', '研究团队'],
  ['本周值得关注的 AI 产品', '产品', '产品团队'],
  ['如何理解模型评测中的能力边界', '教程', '研究团队'],
  ['把复杂问题讲清楚：AI 产品设计的下一步', '观点', '开发者社区'],
].map(([title, category, source], i) => ({
  id: String(i + 1), title, original_title: `Reading AI developments — ${i + 1}`,
  summary: '从能力变化到实际使用，这条资料梳理了值得关注的进展。结合原文与不同信源的观察，帮助读者理解背景、核对信息，再决定是否深入阅读。',
  reason: '把分散的信息放回同一事件中，关注具体变化及其对使用者的影响。',
  url: `https://example.com/articles/${i + 1}`, image_url: '',
  source: { id: `source-${i}`, name: source, tier: i % 2 ? 'media' : 'official', icon_url: '' },
  published_at: `2026-10-30T0${8 - i}:00:00+08:00`, score: 86 - i,
  selected: true, category, tags: ['AI', '开源模型'], story_id: '1', story_others: i === 0 ? 2 : 0,
  story_item_ids: i === 0 ? ['1', '2', '3'] : [String(i + 1)],
}))
const topics = [
  ['openai', 'OpenAI', 'company', '模型、产品与生态动态'],
  ['anthropic', 'Anthropic', 'company', '研究进展与产品更新'],
  ['google', 'Google', 'company', 'AI 相关的最新发布'],
  ['deepseek', 'DeepSeek', 'company', '开放模型与应用实践'],
  ['multimodal', '多模态', 'field', '连接文字、图像、音频与现实世界'],
  ['agents', '智能体', 'field', '从辅助回答到完成任务'],
  ['paper', '论文', 'genre', '理解研究中的问题、方法和发现'],
].map(([slug, name, group, description]) => ({ slug, name, group, description, selected_count: 6, all_count: 12 }))

export async function mockDesignData(page) {
  await page.route('**/api/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let body
    if (path === '/api/items') {
      const category = url.searchParams.get('category')
      body = { items: designItems.filter(item => !category || item.category === category) }
    } else if (path.startsWith('/api/items/')) {
      const item = designItems.find(item => path.endsWith(`/${item.id}`))
      if (!item) return route.fulfill({ status: 404, json: {} })
      body = { ...item, related_items: designItems.slice(1, 3) }
    } else if (path === '/api/meta') body = { categories: ['模型', '产品', '行业', '论文', '教程', '观点'] }
    else if (path === '/api/topics') body = { topics }
    else if (path === '/api/hot') body = { items: designItems.slice(0, 5).map((item, i) => ({ story_id: String(i + 1), title: item.title, heat: 126 - i * 17, trend: i === 0 ? 'rising' : '', sources: [item.source, { id: 'other', name: '研究团队' }] })) }
    else if (path === '/api/reports') body = { items: [{ date: '2026-10-30', headline: designItems[0].title }, { date: '2026-10-29', headline: designItems[1].title }] }
    else if (path.startsWith('/api/reports/')) body = { date: path.split('/').at(-1), headline: designItems[0], highlights: designItems.slice(1, 3), sections: [{ category: '模型', items: [designItems[0]] }, { category: '产品', items: [designItems[1]] }], briefs: designItems.slice(3) }
    else if (path.startsWith('/api/stories/')) body = { id: '1', title: designItems[0].title, items: designItems.slice(0, 3) }
    else return route.fulfill({ status: 404, json: {} })
    return route.fulfill({ json: body })
  })
  await page.route('**/sources.opml', route => route.fulfill({ contentType: 'application/xml', body: '<opml version="2.0"><body><outline title="官方一手"><outline title="官方博客" xmlUrl="https://example.com/feed.xml"/></outline><outline title="媒体与个人"><outline title="开发者社区" htmlUrl="https://example.com/news"/></outline></body></opml>' }))
}

export async function testRedesign(browser, baseURL, screenshotDir) {
  const routes = ['/', '/all', '/all?q=AI', '/hot', '/topics', '/topics/openai', '/reports', '/reports/2026-10-30', '/items/1', '/stories/1', '/favorites', '/more', '/about', '/agent', '/changelog', '/missing']
  let checked = 0
  for (const width of [1536, 390, 320]) for (const theme of ['light', 'dark']) {
    const page = await browser.newPage({ viewport: { width, height: 1000 }, reducedMotion: 'reduce' })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
    await page.addInitScript(theme => localStorage.setItem('vane-theme', theme), theme)
    await mockDesignData(page)
    for (const path of routes) {
      await page.goto(new URL(path, baseURL).href)
      await page.locator('h1').waitFor()
      await page.waitForFunction(() => document.querySelector('.progress').hidden)
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${width} ${theme} ${path}: 不应横向溢出`)
      assert.ok((await page.locator('.brand-mark').evaluateAll(images => images.map(image => image.getAttribute('src')))).every(src => src === '/icon.svg'), '桌面与手机都必须引用原有 Logo 文件')
      assert.equal(await page.locator('.columns-rail').count() > 0, width > 1180 && !['/topics', '/more', '/missing'].includes(path), `${path}: 响应式右栏`)
      const buttons = await page.locator('main button:visible').evaluateAll(elements => elements.map(el => ({ text: el.getAttribute('aria-label') || el.textContent, width: el.getBoundingClientRect().width, height: el.getBoundingClientRect().height })))
      assert.ok(buttons.every(button => button.width >= 24 && button.height >= 24), `${width} ${path}: 操作目标过小 ${JSON.stringify(buttons.filter(b => b.width < 24 || b.height < 24))}`)
      if (screenshotDir && width !== 320 && ['/', '/items/1', '/stories/1', '/topics', '/reports/2026-10-30', '/hot', '/agent', '/more'].includes(path)) {
        const name = path === '/' ? 'home' : path.slice(1).replaceAll('/', '-')
        await page.screenshot({ path: `${screenshotDir}/${name}-${width}-${theme}.png`, fullPage: true })
      }
      checked++
    }
    // 真实用户路径：收藏持久化、搜索、事件入口、详情底栏和键盘帮助。
    await page.goto(baseURL)
    await page.locator('.card').first().waitFor()
    await page.locator('.card').first().getByRole('button', { name: '收藏', exact: true }).click()
    await page.goto(new URL('/favorites', baseURL).href)
    await page.locator('.favorite-rows .row').waitFor()
    assert.match(await page.locator('.favorite-rows').innerText(), /开源模型更新/)
    await page.getByRole('button', { name: '取消收藏' }).click()
    await page.locator('.empty').waitFor()
    await page.goto(baseURL)
    await page.locator('.card').first().waitFor()
    await page.locator('.card-others').first().click()
    await page.locator('#story').waitFor()
    assert.equal(new URL(page.url()).hash, '#story')
    if (width <= 960) {
      const actions = await page.locator('.detail-actions').boundingBox()
      assert.ok(actions.y + actions.height <= 1001, '手机详情操作栏应在视口内')
      await page.getByRole('button', { name: '搜索资料', exact: true }).click()
    }
    const search = page.getByRole('searchbox', { name: '搜索资料' }).filter({ visible: true })
    await search.fill('AI')
    await search.press('Enter')
    await page.getByRole('heading', { name: '搜索「AI」' }).waitFor()
    await page.locator('h1').click()
    await page.keyboard.press('?')
    await page.getByRole('dialog').waitFor()
    await page.keyboard.press('Escape')
    await page.getByRole('dialog').waitFor({ state: 'hidden' })
    assert.deepEqual(errors, [])
    await page.close()
  }
  return { routeThemeViewportChecks: checked }
}

export async function testRedesignStates(browser, baseURL) {
  for (const width of [1181, 961, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 844 }, reducedMotion: 'reduce' })
    await mockDesignData(page)
    let mode = 'loading'
    let release
    const gate = new Promise(resolve => { release = resolve })
    await page.route('**/api/items?*', async route => {
      if (mode === 'loading') await gate
      return route.fulfill({ status: mode === 'failed' ? 500 : 200, json: { items: mode === 'empty' ? [] : designItems } })
    })
    await page.goto(baseURL, { waitUntil: 'domcontentloaded' })
    await page.locator('.skeleton').waitFor()
    assert.equal(await page.locator('.skeleton i').first().evaluate(el => getComputedStyle(el).animationName), 'none', '减少动态效果时骨架不闪烁')
    mode = 'failed'
    release()
    await page.getByRole('heading', { name: '资料暂时加载不出来' }).waitFor()
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${width}: 错误页不溢出`)
    mode = 'empty'
    await page.getByRole('button', { name: '重试', exact: true }).click()
    await page.locator('.empty').waitFor()
    mode = 'ready'
    await page.reload()
    await page.locator('.card').first().waitFor()
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${width}: 断点两侧不溢出`)
    // 验证真实主题控件，而非只预写 storage；浏览器主题色也应随页面一起切换。
    await page.goto(new URL('/more', baseURL).href)
    for (const label of ['深色', '浅色', '跟随系统']) {
      await page.locator('.theme-options label').filter({ hasText: label }).click()
      assert.equal(await page.getByLabel(label, { exact: true }).isChecked(), true)
      if (label !== '跟随系统') {
        const colors = await page.evaluate(() => {
          const probe = document.createElement('span')
          probe.style.color = document.querySelector('meta[name="theme-color"]').content
          document.body.append(probe)
          const result = { meta: getComputedStyle(probe).color, body: getComputedStyle(document.body).backgroundColor }
          probe.remove()
          return result
        })
        assert.equal(colors.meta, colors.body, '浏览器主题色与页面背景保持一致')
      }
    }
    await page.emulateMedia({ colorScheme: 'dark' })
    assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme), 'dark')
    await page.close()
  }
}
