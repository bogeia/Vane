import test from 'node:test'
import assert from 'node:assert/strict'
import { dayLabel, groupByDay, reportDateLabel, reportWeekday, shortDate } from '../src/lib/time.ts'

const now = new Date('2026-10-27T00:00:00Z')
const item = (id: string, published_at: string) => ({ id, published_at })

test('按北京时间分日：跨 UTC 零点的资料分到北京时间的日期', () => {
  const groups = groupByDay([
    item('a', '2026-10-23T16:30:00Z'), // 北京时间 10月24日 00:30
    item('b', '2026-10-23T15:59:00Z'), // 北京时间 10月23日 23:59
    item('c', '2026-10-23T00:10:00+08:00'),
  ], now)
  assert.deepEqual(groups.map(group => [group.key, group.label, group.items.map(entry => entry.id)]), [
    ['2026-10-24', '10月24日周六', ['a']],
    ['2026-10-23', '10月23日周五', ['b', 'c']],
  ])
})

test('分组保持输入顺序，不重新排序', () => {
  const input = [item('x', '2026-10-23T09:00:00+08:00'), item('y', '2026-10-23T11:00:00+08:00'), item('z', '2026-10-22T11:00:00+08:00')]
  assert.deepEqual(groupByDay(input).flatMap(group => group.items.map(entry => entry.id)), ['x', 'y', 'z'])
  assert.deepEqual(groupByDay([]), [])
})

test('日报日期按日期本身显示星期，无效日期不抛错', () => {
  assert.equal(reportDateLabel('2026-10-23', now), '10月23日星期五')
  assert.equal(reportWeekday('2026-10-23'), '星期五')
  for (const date of ['abc', '2026-13-45', '']) {
    assert.equal(reportDateLabel(date), date)
    assert.equal(reportWeekday(date), '')
  }
})

test('日期标签只为非本年日期加年份', () => {
  assert.equal(dayLabel('2026-10-23T00:00:00Z', now), '10月23日周五')
  assert.equal(dayLabel('2025-12-31T00:00:00Z', now), '2025年12月31日周三')
  assert.equal(reportDateLabel('2025-12-31', now), '2025年12月31日星期三')
  assert.equal(shortDate('2026-10-27', now), '10-27')
  assert.equal(shortDate('2025-12-31', now), '2025-12-31')
  assert.equal(shortDate('2026-10-26T16:30:00Z', now), '10-27')
  assert.equal(shortDate('2025-12-30T16:30:00Z', now), '2025-12-31')
})

test('今年按北京时间跨年，日报日期仍按自身年份', () => {
  const newYear = new Date('2026-12-31T16:30:00Z')
  assert.equal(dayLabel('2026-12-31T16:00:00Z', newYear), '1月1日周五')
  assert.equal(dayLabel('2026-12-31T15:59:00Z', newYear), '2026年12月31日周四')
  assert.equal(reportDateLabel('2026-12-31', newYear), '2026年12月31日星期四')
  assert.equal(reportDateLabel('2027-01-01', newYear), '1月1日星期五')
  assert.equal(shortDate('2026-12-31', newYear), '2026-12-31')
  assert.equal(shortDate('2026-12-31T16:00:00Z', newYear), '01-01')
  assert.equal(groupByDay([item('a', '2026-12-31T15:59:00Z')], newYear)[0].label, '2026年12月31日周四')
})
