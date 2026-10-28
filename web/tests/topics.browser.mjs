import assert from 'node:assert/strict'

// 延迟真实响应；检查主题切换时页头与资料加载区一起出现。
export async function testTopicNavigation(page, baseURL) {
  const delayResponse = async route => {
    const response = await route.fetch()
    await new Promise(resolve => setTimeout(resolve, 300))
    await route.fulfill({ response })
  }
  await page.route('**/api/topics', delayResponse)
  await page.route('**/api/items?*', delayResponse)
  try {
    await page.goto(new URL('/topics', baseURL).href)
    const link = page.locator('.topic-card').first()
    await link.waitFor()
    const name = await link.locator('.topic-name').innerText()
    const href = await link.getAttribute('href')
    assert.match(await link.locator('.topic-count').innerText(), /^精选 \d+ · 全部 \d+$/)
    const frames = await page.evaluate(({ name, href }) => new Promise(resolve => {
      const samples = []
      let count = 0
      const sample = () => {
        if (location.pathname === href && !document.querySelector('.topic-grid')) {
          samples.push({ title: document.querySelector('h1')?.textContent ?? null, skeletonTop: document.querySelector('.skeleton')?.getBoundingClientRect().top ?? null })
        }
        if (++count < 60) requestAnimationFrame(sample)
        else resolve(samples)
      }
      requestAnimationFrame(sample)
      document.querySelector(`.topic-card[href="${href}"]`).click()
    }), { name, href })
    assert.ok(frames.length > 0, '必须采到主题详情页面的绘制帧')
    for (const frame of frames) assert.equal(frame.title, name, `主题页头应从首次绘制保持可见：${JSON.stringify(frame)}`)
    const skeletonTops = frames.flatMap(frame => frame.skeletonTop === null ? [] : [frame.skeletonTop])
    assert.ok(skeletonTops.length > 0, '慢响应时必须采到资料加载状态')
    assert.equal(new Set(skeletonTops).size, 1, '资料加载区不能被后插入的页头推移')
  } finally {
    await page.unroute('**/api/topics', delayResponse)
    await page.unroute('**/api/items?*', delayResponse)
  }
}
