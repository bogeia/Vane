import assert from 'node:assert/strict'

const CATEGORIES = ['模型', '产品', '行业', '论文', '教程', '观点']
const makeItem = (index, category) => ({
  id: `shake-${index}`,
  title: `测试资料 ${index}：分类切换时保持精选列表阅读位置稳定`,
  original_title: '',
  summary: '这条固定夹具用于覆盖较长精选列表被筛成少量或空结果时的页面布局。切换分类应更新列表内容，同时让当前阅读位置保持稳定。'.repeat(2),
  reason: '固定浏览器夹具。',
  url: 'https://example.com/item',
  image_url: '',
  source: { id: 'fixture', name: '测试信源', tier: 'official', icon_url: '' },
  published_at: `2026-10-${String(20 - Math.floor(index / 6)).padStart(2, '0')}T12:00:00Z`,
  score: 80,
  selected: true,
  category,
  tags: [],
  story_id: '',
  story_others: 0,
  story_item_ids: [],
})

// Real browser/layout regression for the selected feed. The fixtures keep result sizes repeatable.
export async function testFilterShake(page, baseURL) {
  const all = Array.from({ length: 20 }, (_, i) => makeItem(i, CATEGORIES[i % CATEGORIES.length]))
  const byCategory = {
    模型: all.slice(0, 3),
    行业: all.filter(item => item.category === '行业').slice(0, 2),
    论文: [],
  }
  await page.route('**/api/meta', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ categories: CATEGORIES }),
  }))
  await page.route('**/api/items**', async route => {
    const category = new URL(route.request().url()).searchParams.get('category') || ''
    const items = category ? (byCategory[category] || all.filter(item => item.category === category)) : all
    await new Promise(resolve => setTimeout(resolve, 45))
    try { await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ items }) }) } catch { /* A newer filter may cancel this response. */ }
  })

  try {
    await page.goto(new URL('/', baseURL).href)
    await page.getByRole('button', { name: '全部分类', exact: true }).waitFor()
    await page.waitForFunction(() => document.querySelectorAll('.card').length === 20)
    const transitions = []
    for (const [label, expected] of [['论文', 0], ['行业', 2], ['模型', 3], ['全部分类', 20], ['论文', 0], ['全部分类', 20]]) {
      await page.evaluate(() => window.scrollTo(0, 40))
      const before = await page.evaluate(() => ({ y: scrollY, maxY: document.documentElement.scrollHeight - innerHeight }))
      assert.equal(before.y, 40, '夹具必须让分类控件可见且页面可滚动')
      const frames = await page.evaluate(({ label, expected }) => new Promise(resolve => {
        const samples = []
        let stable = 0
        let ticks = 0
        const sample = () => {
          const point = selector => {
            const { x, y } = document.querySelector(selector).getBoundingClientRect()
            return { x, y }
          }
          samples.push({ y: scrollY, cards: document.querySelectorAll('.card').length, pills: point('.category-pills'), tools: point('.feed-tools') })
          stable = samples.at(-1).cards === expected ? stable + 1 : 0
          if (stable >= 3 || ++ticks >= 120) resolve(samples)
          else requestAnimationFrame(sample)
        }
        requestAnimationFrame(sample)
        ;[...document.querySelectorAll('.category-pills button')].find(button => button.innerText === label).click()
      }), { label, expected })
      const after = frames.at(-1)
      assert.equal(after.cards, expected, `${label}结果数量应为 ${expected}`)
      const delta = key => ({
        x: Math.max(...frames.map(frame => frame[key].x)) - Math.min(...frames.map(frame => frame[key].x)),
        y: Math.max(...frames.map(frame => frame[key].y)) - Math.min(...frames.map(frame => frame[key].y)),
      })
      const verticalDelta = Math.max(...frames.map(frame => frame.y)) - Math.min(...frames.map(frame => frame.y))
      const pillsDelta = delta('pills')
      const toolsDelta = delta('tools')
      transitions.push({ label, before, endY: after.y, verticalDelta, pillsDelta, toolsDelta, cards: after.cards, frames: frames.length })
      assert.equal(verticalDelta, 0, `${label}切换时页面跳动：${before.y} → ${after.y}`)
      assert.deepEqual(pillsDelta, { x: 0, y: 0 }, `${label}分类按钮位置不应变化`)
      assert.deepEqual(toolsDelta, { x: 0, y: 0 }, `${label}工具栏位置不应变化`)
    }
    await page.evaluate(() => window.scrollTo(0, 40))
    const before = await page.evaluate(() => ({ y: scrollY, maxY: document.documentElement.scrollHeight - innerHeight }))
    const rapid = await page.evaluate(({ labels, expected }) => new Promise(resolve => {
      const samples = []
      let next = 0
      let stable = 0
      let timer
      const clickNext = () => {
        const label = labels[next++]
        ;[...document.querySelectorAll('.category-pills button')].find(button => button.innerText === label).click()
        if (next < labels.length) timer = setTimeout(clickNext, 20)
      }
      const sample = () => {
        const pills = document.querySelector('.category-pills').getBoundingClientRect()
        const tools = document.querySelector('.feed-tools').getBoundingClientRect()
        samples.push({ y: scrollY, cards: document.querySelectorAll('.card').length, pills: { x: pills.x, y: pills.y }, tools: { x: tools.x, y: tools.y } })
        stable = next === labels.length && samples.at(-1).cards === expected ? stable + 1 : 0
        if (stable >= 3 || samples.length >= 180) { clearTimeout(timer); resolve(samples) }
        else requestAnimationFrame(sample)
      }
      requestAnimationFrame(sample)
      clickNext()
    }), { labels: ['论文', '行业', '模型', '论文', '全部分类', '行业'], expected: 2 })
    const last = rapid.at(-1)
    assert.equal(last.cards, 2, '快速轮换后应显示最后选择的少量分类')
    assert.ok(rapid.every(frame => frame.y === before.y), `快速分类轮换时页面跳动：scrollY ${Math.min(...rapid.map(frame => frame.y))}…${Math.max(...rapid.map(frame => frame.y))}，起点 ${before.y}`)
    const rapidVerticalDelta = Math.max(...rapid.map(frame => frame.y)) - Math.min(...rapid.map(frame => frame.y))
    const rapidPillsDelta = { x: Math.max(...rapid.map(frame => frame.pills.x)) - Math.min(...rapid.map(frame => frame.pills.x)), y: Math.max(...rapid.map(frame => frame.pills.y)) - Math.min(...rapid.map(frame => frame.pills.y)) }
    const rapidToolsDelta = { x: Math.max(...rapid.map(frame => frame.tools.x)) - Math.min(...rapid.map(frame => frame.tools.x)), y: Math.max(...rapid.map(frame => frame.tools.y)) - Math.min(...rapid.map(frame => frame.tools.y)) }
    assert.equal(rapidVerticalDelta, 0, '快速轮换时页面不应跳动')
    assert.deepEqual(rapidPillsDelta, { x: 0, y: 0 }, '快速轮换时分类按钮位置不应变化')
    assert.deepEqual(rapidToolsDelta, { x: 0, y: 0 }, '快速轮换时工具栏位置不应变化')
    transitions.push({ label: '快速轮换', before, endY: last.y, verticalDelta: rapidVerticalDelta, pillsDelta: rapidPillsDelta, toolsDelta: rapidToolsDelta, cards: last.cards, frames: rapid.length })

    await page.evaluate(() => window.scrollTo(0, 0))
    await page.getByRole('button', { name: '全部分类', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.card').length === 20)
    const reset = await page.evaluate(() => ({ y: scrollY, minHeight: document.querySelector('.columns-main > div')?.style.minHeight, maxY: document.documentElement.scrollHeight - innerHeight }))
    assert.equal(reset.y, 0)
    assert.equal(reset.minHeight, '0px', '回顶后切换分类应清掉临时占位高度')
    assert.ok(reset.maxY > 1000, '长列表恢复后页面应继续正常增长')
    transitions.push({ label: '回顶后恢复长列表', ...reset, cards: 20 })
    return transitions
  } finally {
    await page.unroute('**/api/items**')
    await page.unroute('**/api/meta')
  }
}
