import test from 'node:test'
import assert from 'node:assert/strict'
import { isDetailPath, navKeyFor, tabKeyFor } from '../src/shell/nav.ts'

test('侧栏和底部标签按 spec 的表高亮', () => {
  const cases: [string, ReturnType<typeof navKeyFor>, ReturnType<typeof tabKeyFor>][] = [
    ['/', 'selected', 'selected'],
    ['/all', 'all', 'selected'],
    ['/hot', 'hot', 'hot'],
    ['/stories/12', 'hot', 'hot'],
    ['/reports', 'reports', 'reports'],
    ['/reports/2026-10-23', 'reports', 'reports'],
    ['/topics', 'topics', 'more'],
    ['/topics/agent', 'topics', 'more'],
    ['/favorites', 'favorites', 'more'],
    ['/about', 'about', 'more'],
    ['/changelog', 'changelog', 'more'],
    ['/agent', 'agent', 'more'],
    ['/more', null, 'more'],
    ['/items/240', null, null],
    ['/nope', null, null],
  ]
  for (const [path, nav, tab] of cases) {
    assert.equal(navKeyFor(path), nav, path)
    assert.equal(tabKeyFor(path, navKeyFor(path)), tab, path)
  }
})

test('资料详情由页面决定高亮：入选是精选，没入选是全部动态', () => {
  assert.equal(tabKeyFor('/items/240', 'selected'), 'selected')
  assert.equal(tabKeyFor('/items/240', 'all'), 'selected')
})

test('只有资料详情和事件页按详情页处理', () => {
  assert.equal(isDetailPath('/items/240'), true)
  assert.equal(isDetailPath('/stories/12'), true)
  for (const path of ['/', '/all', '/reports/2026-10-23', '/topics/agent', '/items', '/items/240/extra']) assert.equal(isDetailPath(path), false, path)
})
