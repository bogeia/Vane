import assert from 'node:assert/strict'

const topics = [
  { slug: 'alpha', name: 'Alpha', group: 'company', description: 'Alpha 主题', selected_count: 2, all_count: 3 },
  { slug: 'beta', name: 'Beta', group: 'company', description: 'Beta 主题', selected_count: 0, all_count: 3 },
]

// 完全模拟公开读取，不连接数据库；同时检查 React 消费者交接和首帧。
export async function testSharedReading(browser, baseURL) {
  for (const width of [1440, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } })
    const counts = new Map()
    let failTopics = false
    let releaseReports
    const reportsGate = new Promise(resolve => { releaseReports = resolve })
    let releaseBetaItems
    const betaItemsGate = new Promise(resolve => { releaseBetaItems = resolve })
    await page.route('**/api/**', async route => {
      const url = new URL(route.request().url())
      const path = url.pathname
      counts.set(path, (counts.get(path) ?? 0) + 1)
      if (path === '/api/reports') await reportsGate
      if (path === '/api/items' && url.searchParams.get('topic') === 'beta') await betaItemsGate
      await new Promise(resolve => setTimeout(resolve, path === '/api/items' ? 250 : 70))
      const body = path === '/api/topics' ? { topics } : path === '/api/reports'
        ? { items: [{ date: '2026-10-28', headline: '今日日报' }] }
        : path === '/api/meta' ? { categories: [] } : { items: [] }
      await route.fulfill({ status: path === '/api/topics' && failTopics ? 500 : 200, json: body })
    })
    await page.goto(new URL('/reports', baseURL).href, { waitUntil: 'domcontentloaded' })
    // 响应由断言释放，避免快速机器在 goto 返回前已结束进度。
    try {
      await page.locator('.progress:not([hidden])').waitFor()
    } finally {
      releaseReports()
    }
    await page.locator('.issue-list').waitFor()
    await page.waitForFunction(() => document.querySelector('.progress').hidden)
    assert.equal(counts.get('/api/reports'), 1, 'Shell 与日报主页面共享初次请求')
    await page.waitForTimeout(300)
    counts.clear()
    await page.goto(new URL('/topics/alpha', baseURL).href)
    await page.getByRole('heading', { name: 'Alpha', exact: true }).waitFor()
    await page.waitForTimeout(300)
    assert.equal(counts.get('/api/topics'), 1, '主题主页面与宽屏右栏共享初次请求')
    assert.equal(counts.get('/api/hot') ?? 0, width === 390 ? 0 : 1)
    await page.getByRole('button', { name: '全部', exact: true }).click()
    await page.locator('.sidebar a[href="/topics"]').evaluate(link => link.click())
    await page.locator('.topic-card').first().waitFor()
    // 采样结束才释放目标主题的资料，保证骨架观察不依赖机器速度。
    const frames = await page.evaluate(() => new Promise(resolve => {
      const samples = []
      let count = 0
      function sample() {
        if (location.pathname === '/topics/beta' && !document.querySelector('.topic-grid')) samples.push({ title: document.querySelector('h1')?.textContent, top: document.querySelector('.skeleton')?.getBoundingClientRect().top })
        if (++count < 30) requestAnimationFrame(sample)
        else resolve(samples)
      }
      requestAnimationFrame(sample)
      document.querySelector('.topic-card[href="/topics/beta"]').click()
    })).finally(() => releaseBetaItems())
    assert.ok(frames.length > 0)
    assert.ok(frames.every(frame => frame.title === 'Beta'), `站内进入主题第一帧就显示页头：${JSON.stringify(frames)}`)
    const skeletonTops = frames.flatMap(frame => frame.top === undefined ? [] : [frame.top])
    assert.ok(skeletonTops.length > 0, '慢资料响应必须展示骨架')
    assert.equal(new Set(skeletonTops).size, 1, '页头不能后插入推动资料骨架')
    assert.equal(await page.getByRole('button', { name: '全部', exact: true }).getAttribute('aria-pressed'), 'true', '无精选主题默认全部')
    // 离开所有主题消费者后，重新进入必须请求新数据，首次失败仍可重试。
    await page.locator('.sidebar a[href="/about"]').evaluate(link => link.click())
    await page.getByRole('heading', { name: '关于 Vane', exact: true }).waitFor()
    assert.equal(await page.locator('.progress').evaluate(bar => bar.hidden), true, 'Shell后台再校验不触发主进度条')
    failTopics = true
    await page.locator('.sidebar a[href="/topics"]').evaluate(link => link.click())
    await page.getByRole('button', { name: /重试/ }).waitFor()
    failTopics = false
    await page.getByRole('button', { name: /重试/ }).click()
    await page.locator('.topic-card').first().waitFor()
    await page.close()
  }
  // 窄屏首页不因右栏读取主题或热点。
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  const paths = []
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    paths.push(path)
    return route.fulfill({ json: path === '/api/meta' ? { categories: [] } : { items: [] } })
  })
  await page.goto(baseURL)
  await page.waitForTimeout(300)
  assert.ok(!paths.includes('/api/topics') && !paths.includes('/api/hot'))
  await page.close()
}
