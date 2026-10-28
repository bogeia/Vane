import assert from 'node:assert/strict'

// 观察新页面实际出现的每一帧，最终回顶仍可能漏掉中间的一次跳动。
export async function testNavigationScroll(page, baseURL) {
  await page.goto(new URL('/agent', baseURL).href)
  await page.getByRole('heading', { name: '接入 Agent', exact: true }).waitFor()
  await page.evaluate(() => window.scrollTo({ top: document.documentElement.scrollHeight, behavior: 'instant' }))
  assert.ok(await page.evaluate(() => scrollY > innerHeight * 1.5), '前置页面必须滚过回顶按钮门槛')
  await page.getByRole('button', { name: '回到顶部', exact: true }).waitFor({ state: 'visible' })
  const frames = await page.evaluate(() => new Promise(resolve => {
    const samples = []
    let count = 0
    const sample = () => {
      const heading = document.querySelector('h1')
      if (heading?.textContent === '关于 Vane') samples.push({ y: scrollY, top: heading.getBoundingClientRect().top, backToTop: !document.querySelector('.back-to-top')?.hidden })
      if (++count < 30) requestAnimationFrame(sample)
      else resolve(samples)
    }
    requestAnimationFrame(sample)
    document.querySelector('.sidebar a[href="/about"]').click()
  }))
  assert.ok(frames.length > 0, '必须采到新页面的实际绘制帧')
  for (const frame of frames) {
    assert.equal(frame.y, 0, `新页面首帧就应回顶：${JSON.stringify(frame)}`)
    assert.equal(frame.backToTop, false, '回到顶部按钮不能沿用上一页的可见状态')
  }
}

export async function testTopNavigationScroll(page, baseURL) {
  await page.goto(new URL('/', baseURL).href)
  await page.locator('.card').first().waitFor()
  for (const [href, title, content] of [['/all', '全部动态', '.row'], ['/reports', '日报', '.issue-list']]) {
    await page.evaluate(() => window.scrollTo({ top: document.documentElement.scrollHeight, behavior: 'instant' }))
    await page.waitForFunction(() => document.querySelector('.back-to-top').hidden === !(scrollY > innerHeight * 1.5))
    const frames = await page.evaluate(({ href, title }) => new Promise(resolve => {
      const samples = []
      let count = 0
      const sample = () => {
        if (document.querySelector('h1')?.textContent === title) samples.push({ y: scrollY, backToTop: !document.querySelector('.back-to-top').hidden })
        if (++count < 30) requestAnimationFrame(sample)
        else resolve(samples)
      }
      requestAnimationFrame(sample)
      document.querySelector(`.sidebar a[href="${href}"]`).click()
    }), { href, title })
    assert.ok(frames.length > 0, `必须采到${title}页面`)
    for (const frame of frames) {
      assert.equal(frame.y, 0, `${title}新页面首帧应回顶`)
      assert.equal(frame.backToTop, false, `${title}不能沿用上一页的回顶按钮`)
    }
    await page.locator(content).first().waitFor()
  }
}
