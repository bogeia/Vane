import assert from 'node:assert/strict'

// 由 Playwright page 调用；只替换公开 OPML 接口，验证读者看到的信源与链接。
export async function testAboutSources(page, baseURL) {
  await page.route('**/sources.opml', route => route.fulfill({
    contentType: 'application/xml',
    body: `<?xml version="1.0"?><opml version="2.0"><body><outline title="官方一手">
      <outline title="RSS 示例" xmlUrl="https://example.com/feed.xml" htmlUrl="https://example.com/" />
      <outline title="网页列表示例" htmlUrl="https://example.com/news" />
      <outline title="模型发布示例" htmlUrl="https://huggingface.co/example" />
      <outline title="无效信源" htmlUrl="javascript:alert(1)" />
    </outline></body></opml>`,
  }))
  try {
    await page.goto(new URL('/about', baseURL).href)
    const group = page.getByRole('region', { name: '官方一手', exact: true })
    await group.waitFor()
    assert.equal(await group.locator('li').count(), 3)
    assert.equal(await group.getByRole('link', { name: 'RSS 示例 的订阅地址（新标签页）', exact: true }).getAttribute('href'), 'https://example.com/feed.xml')
    for (const [name, url] of [['网页列表示例', 'https://example.com/news'], ['模型发布示例', 'https://huggingface.co/example']]) {
      const link = group.getByRole('link', { name: `${name} 的官网（新标签页）`, exact: true })
      assert.equal(await link.getAttribute('href'), url)
      assert.equal(await link.innerText(), '官网 ↗')
    }
    assert.match(await group.innerText(), /3 个/)
    assert.equal(await page.getByText('无效信源', { exact: true }).count(), 0)
  } finally {
    await page.unroute('**/sources.opml')
  }
}
