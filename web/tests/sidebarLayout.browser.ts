import assert from 'node:assert/strict'

// Called by the browser runner with a Playwright page and the Vite base URL.
export async function testSidebarLayout(page, baseURL) {
  const pageErrors: Error[] = []
  const onPageError = (error: Error) => pageErrors.push(error)
  page.on('pageerror', onPageError)
  const item = {
    id: 'item-1', title: '测试资料', original_title: '', summary: '测试摘要', reason: '推荐理由',
    url: 'https://example.com/item', image_url: '',
    source: { id: 'source-1', name: '测试信源', tier: 'official', icon_url: '' },
    published_at: '2026-10-28T08:00:00+08:00', score: 90, selected: true,
    category: 'industry', tags: [], story_id: 'story-1', story_others: 0, story_item_ids: ['item-1'],
  }
  const reports = [
    { date: '2026-10-28', headline: '日报头条' },
    { date: '2026-10-27', headline: '前一期' },
  ]
  const topics = [
    { slug: 'openai', name: 'OpenAI', group: 'company', description: '', selected_count: 2, all_count: 3 },
    { slug: 'anthropic', name: 'Anthropic', group: 'company', description: '', selected_count: 1, all_count: 1 },
    ...Array.from({ length: 12 }, (_, index) => ({ slug: `topic-${index}`, name: `较长的测试主题名称 ${index}`, group: 'field', description: '', selected_count: 1, all_count: 1 })),
  ]
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    const body = path === '/api/topics' ? { topics }
      : path === '/api/reports' ? { items: reports }
      : path === '/api/reports/2026-10-28' ? { date: '2026-10-28', headline: item, highlights: [{ ...item, id: 'highlight-1', title: '日报看点' }], sections: [{ category: '行业', items: [item] }], briefs: [{ ...item, id: 'brief-1', title: '日报快讯' }] }
        : path === '/api/hot' ? { items: Array.from({ length: 6 }, (_, index) => ({ story_id: `story-${index}`, title: '这是一条用于验证长右栏仍能滚动到末尾主题的热点标题。'.repeat(3), heat: 10, sources: [{ name: '测试信源' }] })) }
          : path === '/api/items/item-1' ? { ...item, related_items: [] }
            : path === '/api/stories/story-1' ? { id: 'story-1', title: '测试事件', items: [item] }
              : path.startsWith('/api/items') ? { items: [item] }
                : path === '/api/meta' ? { categories: ['模型'] } : {}
    return route.fulfill({ json: body })
  })

  try {
    for (const height of [900, 800]) {
      await page.setViewportSize({ width: 1440, height })
      await page.goto(new URL('/', baseURL).href)
      await page.locator('.columns-rail .topic-chips a').last().waitFor()
      const rail = page.locator('.columns-rail')
      const before = await rail.evaluate(element => ({ client: element.clientHeight, scroll: element.scrollHeight, top: getComputedStyle(element).top }))
      assert.equal(before.top, '20px')
      assert.ok(before.scroll > height - 40, `${height}px fixture must exceed the available rail height`)
      assert.ok(before.client <= height - 40, `${height}px rail must fit the viewport`)
      await rail.evaluate(element => { element.scrollTop = element.scrollHeight })
      const end = await rail.evaluate(element => {
        const bounds = element.getBoundingClientRect()
        const last = element.querySelector('.topic-chips a:last-child')!.getBoundingClientRect()
        return { scrollTop: element.scrollTop, lastTop: last.top, lastBottom: last.bottom, top: bounds.top, bottom: bounds.bottom }
      })
      assert.ok(end.scrollTop > 0, 'rail scrolls independently')
      assert.ok(end.lastTop >= end.top && end.lastBottom <= end.bottom + 1, 'last topic is reachable inside the rail')
      assert.ok(end.bottom <= height - 10, 'rail bottom stays inside the viewport')
    }
    for (const [path, ready] of [
      ['/topics/openai', 'h1'], ['/favorites', 'h1'], ['/reports', '.issue-list'], ['/reports/2026-10-28', '#r-headline'],
      ['/items/item-1', '#detail-summary'], ['/stories/story-1', '.timeline li'],
    ]) {
      await page.setViewportSize({ width: 1440, height: 900 })
      await page.goto(new URL(path, baseURL).href)
      await page.locator(ready).waitFor()
      const rail = page.locator('.columns-rail')
      await rail.waitFor()
      assert.equal(await rail.evaluate(element => Math.round(element.getBoundingClientRect().width)), 296, `${path} rail width`)
      assert.equal(await rail.evaluate(element => Math.round(element.getBoundingClientRect().right)), 1389, `${path} rail edge`)
      if (path !== '/reports' && !path.startsWith('/reports/')) await page.locator('.columns-rail [aria-labelledby="rail-report"]').waitFor()
    }
    await page.goto(new URL('/topics/openai', baseURL).href)
    await page.locator('.columns-rail a[href="/topics/anthropic"]').waitFor()
    const topicLinks = await page.locator('.columns-rail a').evaluateAll(links => links.map(link => link.getAttribute('href')))
    assert.ok(topicLinks.includes('/topics/anthropic'))
    assert.ok(!topicLinks.includes('/topics/openai'))

    await page.goto(new URL('/reports', baseURL).href)
    await page.locator('.columns-rail .topic-chips a').last().waitFor()
    assert.equal(await page.locator('.columns-rail [aria-labelledby="rail-report"]').count(), 0, 'report list omits the entire duplicate report panel')
    await page.goto(new URL('/reports/2026-10-28', baseURL).href)
    await page.locator('.toc a[href="#r-headline"]').waitFor()
    for (const href of ['#r-headline', '#r-highlights', '#r-section-0', '#r-briefs']) {
      assert.ok(await page.locator(`.toc a[href="${href}"]`).count(), `daily TOC ${href}`)
      assert.ok(await page.locator(`[id="${href.slice(1)}"]`).count(), `daily target ${href}`)
    }

    for (const [path, href] of [['/about', '#about-what'], ['/agent', '#agent-mcp'], ['/changelog', '#release-v0.1.0']]) {
      await page.goto(new URL(path, baseURL).href)
      assert.ok(await page.locator(`.toc a[href="${href}"]`).count(), `${path} TOC link`)
      assert.ok(await page.locator(`[id="${href.slice(1)}"]`).count(), `${path} TOC target`)
      await page.locator(`.toc a[href="${href}"]`).click()
      assert.equal(await page.evaluate(() => location.hash), href)
      assert.ok(await page.locator(`[id="${href.slice(1)}"]`).evaluate(element => element.getBoundingClientRect().top < innerHeight))
    }

    const narrowHotRequests: string[] = []
    const onRequest = (request: { url(): string }) => { if (new URL(request.url()).pathname === '/api/hot') narrowHotRequests.push(request.url()) }
    page.on('request', onRequest)
    for (const width of [1180, 390]) {
      await page.setViewportSize({ width, height: 900 })
      for (const path of ['/topics/openai', '/favorites', '/reports', '/reports/2026-10-28', '/items/item-1', '/stories/story-1', '/about', '/agent', '/changelog']) {
        await page.goto(new URL(path, baseURL).href)
        assert.equal(await page.locator('.columns-rail').count(), 0, `${width}px ${path} has no rail`)
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${width}px ${path} has no horizontal overflow`)
      }
    }
    page.off('request', onRequest)
    assert.deepEqual(narrowHotRequests, [])
    assert.deepEqual(pageErrors.map(error => error.message), [])
  } finally {
    page.off('pageerror', onPageError)
    await page.unroute('**/api/**')
  }
}
