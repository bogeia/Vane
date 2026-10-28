import assert from 'node:assert/strict'

// 资料头像只依赖 API icon_url；主题品牌继续使用既有静态资源。
export async function testSourceLogos(page, baseURL) {
  const publishers = [
    ['rss-cursor-changelog', 'cursor'], ['rss-openrouter-blog', 'openrouter'],
    ['web-anthropic-news', 'anthropic'], ['web-kimi-blog', 'kimi'],
    ['hf-qwen', 'qwen'], ['hf-deepseek', 'deepseek'], ['hf-minimax', 'minimax'],
    ['hf-zhipu', 'zhipu'], ['hf-kimi', 'kimi'],
  ]
  const items = [...publishers, ['unknown-source', 'Unknown']].map(([id, name], index) => ({
    id: String(index), title: name, original_title: name, summary: '测试摘要', reason: '',
    url: 'https://example.com/', image_url: '', source: { id, name, tier: 'official', icon_url: name === 'Unknown' ? '' : `/api/source-icons/${id}?v=fixture` },
    published_at: '2026-10-27T08:00:00Z', score: 90, selected: true, category: '模型',
    tags: [], story_id: '', story_others: 0, story_item_ids: [],
  }))
  const topics = [...new Set(publishers.map(([, name]) => name))].map(slug => ({
    slug, name: slug, group: 'company', description: '', selected_count: items.length, all_count: items.length,
  }))
  await page.route('**/api/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.startsWith('/api/source-icons/')) return route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><circle cx="128" cy="128" r="120" fill="blue"/></svg>' })
    const body = path === '/api/topics' ? { topics }
      : path === '/api/meta' ? { categories: [] }
      : path === '/api/items' ? { items, next_cursor: '' }
      : path === '/api/reports/2026-10-27' ? { date: '2026-10-27', headline: items[0], highlights: [], sections: [{ category: '模型', items }], briefs: [] }
      : { items: [] }
    return route.fulfill({ json: body })
  })
  const checkImages = async selector => {
    await page.waitForFunction(selector => [...document.querySelectorAll(selector)].every(img => img.complete && img.naturalWidth > 0), selector)
  }
  try {
    for (const path of ['/', ...topics.map(topic => `/topics/${topic.slug}`), '/reports/2026-10-27']) {
      await page.goto(new URL(path, baseURL).href)
      await page.locator('.card').first().waitFor()
      assert.equal(await page.locator('.source-avatar img').count(), publishers.length + (path.startsWith('/reports/') ? 1 : 0), path)
      for (const [id, name] of publishers) assert.ok(await page.locator(`.source-avatar img[src="/api/source-icons/${id}?v=fixture"]`).count() > 0, `${path}: ${name}`)
      await checkImages('.source-avatar img')
      assert.equal(await page.locator('.source-avatar').last().innerText(), 'U', path)
    }
    await page.goto(new URL('/topics', baseURL).href)
    await page.locator('.topic-logo').first().waitFor()
    assert.equal(await page.locator('.topic-logo').count(), topics.length)
    await checkImages('.topic-logo')
    for (const path of ['/all', '/all?q=deepseek']) {
      await page.goto(new URL(path, baseURL).href)
      await page.locator('.row').first().waitFor()
      assert.equal(await page.locator('.row').count(), items.length)
      assert.equal(await page.locator('.source-avatar').count(), 0)
    }
    await page.goto(new URL('/', baseURL).href)
    await page.locator('.source-avatar img').first().waitFor()
    await page.locator('.source-avatar img').first().dispatchEvent('error')
    assert.equal(await page.locator('.source-avatar').first().innerText(), 'c')
  } finally {
    await page.unroute('**/api/**')
  }
}
