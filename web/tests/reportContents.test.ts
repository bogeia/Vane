import test from 'node:test'
import assert from 'node:assert/strict'
import { reportContents, reportItemAnchor } from '../src/lib/reportContents.ts'
import type { Item } from '../src/items/types.ts'

const item = (id: string, summary = `摘要 ${id}`, reason = `理由 ${id}`) => ({ id, title: id, summary, reason }) as Item

test('日报跨头条、看点、分类和快讯按资料ID唯一呈现，保留所有独有资料', () => {
  const report = { date: '2026-11-03', headline: item('headline'), highlights: [item('section'), item('highlight'), item('headline')], sections: [{ category: '模型', items: [item('section')] }, { category: '产品', items: [item('section'), item('second')] }], briefs: [item('section'), item('brief'), item('brief')] }
  const contents = reportContents(report)
  assert.equal(contents.count, 5)
  assert.deepEqual(contents.sections.map(section => [section.category, section.items.map(item => item.id)]), [['本期重点', ['headline', 'highlight']], ['模型', ['section']], ['产品', ['second']]])
  assert.deepEqual(contents.briefs.map(item => item.id), ['brief'])
  const ids = [...contents.sections.flatMap(section => section.items), ...contents.briefs].map(item => reportItemAnchor(item.id))
  assert.equal(new Set(ids).size, ids.length)
  for (const linked of [report.headline, ...report.highlights]) assert.ok(ids.includes(reportItemAnchor(linked.id)))
})

test('同一资料只展示一次，仍保留入口中的不同摘要和理由且不修改API数据', () => {
  const original = item('same', '分类正文', '分类理由')
  const contents = reportContents({ date: '', headline: item('same', '头条正文', '头条理由'), highlights: [item('same', '头条正文', '')], sections: [{ category: '模型', items: [original] }], briefs: [item('same', '', '快讯理由')] })
  assert.equal(contents.count, 1)
  assert.equal(contents.sections[0].items[0].summary, '分类正文\n\n头条正文')
  assert.equal(contents.sections[0].items[0].reason, '分类理由\n\n头条理由\n\n快讯理由')
  assert.equal(original.summary, '分类正文')
  assert.equal(original.reason, '分类理由')
})

test('空刊与非数字ID具有稳定无冲突的锚点', () => {
  assert.deepEqual(reportContents({ date: '', headline: null, highlights: [], sections: [], briefs: [] }), { sections: [], briefs: [], count: 0 })
  assert.equal(reportItemAnchor('a b'), 'r-item-a%20b')
  assert.notEqual(reportItemAnchor('a b'), reportItemAnchor('a%20b'))
  assert.equal(decodeURIComponent(encodeURIComponent(reportItemAnchor('a b'))), reportItemAnchor('a b'))
})
