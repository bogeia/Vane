import test from 'node:test'
import assert from 'node:assert/strict'
import { nextIndex, shortcutAction, shortcutPage, type KeyInput } from '../src/lib/shortcuts.ts'

const press = (key: string, extra: Partial<KeyInput> = {}) => shortcutAction({ key, ctrl: false, meta: false, alt: false, editable: false, page: 'list', helpOpen: false, searchOpen: false, ...extra })

test('页面类型按 spec 划分', () => {
  for (const path of ['/', '/all', '/favorites', '/hot', '/topics/agent', '/reports/2026-10-23', '/stories/88']) assert.equal(shortcutPage(path), 'list', path)
  assert.equal(shortcutPage('/items/240'), 'detail')
  for (const path of ['/topics', '/reports', '/more', '/about', '/changelog', '/nope', '/items/1/x']) assert.equal(shortcutPage(path), 'other', path)
})

test('每个按键在列表页、详情页、其他页面的动作', () => {
  const table: [string, string | null, string | null, string | null][] = [
    ['/', 'focus-search', 'focus-search', null],
    ['j', 'next', null, null],
    ['k', 'prev', null, null],
    ['o', 'open-original', 'open-original', null],
    ['f', 'favorite', 'favorite', null],
    ['?', 'toggle-help', 'toggle-help', 'toggle-help'],
    ['Escape', null, 'back', null],
    ['Enter', null, null, null],
    ['x', null, null, null],
    ['J', null, null, null],
  ]
  for (const [key, list, detail, other] of table) {
    assert.equal(press(key, { page: 'list' }), list, `list ${key}`)
    assert.equal(press(key, { page: 'detail' }), detail, `detail ${key}`)
    assert.equal(press(key, { page: 'other' }), other, `other ${key}`)
  }
})

test('焦点在输入框里时都不处理；Ctrl、⌘、Alt 组合键不处理', () => {
  for (const key of ['/', 'j', 'k', 'o', 'f', '?']) {
    assert.equal(press(key, { editable: true }), null, `editable ${key}`)
    assert.equal(press(key, { ctrl: true }), null, `ctrl ${key}`)
    assert.equal(press(key, { meta: true }), null, `meta ${key}`)
    assert.equal(press(key, { alt: true }), null, `alt ${key}`)
  }
  assert.equal(press('Escape', { page: 'detail', editable: true }), null)
  assert.equal(press('Escape', { page: 'detail', meta: true }), null)
})

test('Esc 先关帮助，再关搜索框，最后才在资料详情返回', () => {
  assert.equal(press('Escape', { page: 'detail', helpOpen: true, searchOpen: true }), 'close-help')
  assert.equal(press('Escape', { page: 'detail', searchOpen: true }), 'close-search')
  assert.equal(press('Escape', { page: 'detail' }), 'back')
  // 焦点在手机搜索框里时，Esc 仍然能关掉它。
  assert.equal(press('Escape', { page: 'list', searchOpen: true, editable: true }), 'close-search')
})

test('? 在其他页面也能打开帮助；帮助打开时只响应 ? 和 Esc', () => {
  assert.equal(press('?', { page: 'other' }), 'toggle-help')
  assert.equal(press('?', { helpOpen: true }), 'toggle-help')
  for (const key of ['/', 'j', 'k', 'o', 'f']) assert.equal(press(key, { helpOpen: true }), null, key)
})

test('当前条移动：没有当前条时 j 选第一条、k 选最后一条，到头停住', () => {
  assert.equal(nextIndex(-1, 5, 1), 0)
  assert.equal(nextIndex(-1, 5, -1), 4)
  assert.equal(nextIndex(2, 5, 1), 3)
  assert.equal(nextIndex(2, 5, -1), 1)
  assert.equal(nextIndex(4, 5, 1), 4)
  assert.equal(nextIndex(0, 5, -1), 0)
})

test('当前条移动：列表为空返回 -1，列表变短后当前条越界按没有当前条处理', () => {
  assert.equal(nextIndex(-1, 0, 1), -1)
  assert.equal(nextIndex(3, 0, -1), -1)
  assert.equal(nextIndex(7, 5, 1), 0)
  assert.equal(nextIndex(7, 5, -1), 4)
})
