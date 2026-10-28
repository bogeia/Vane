// 页面上的时间一律按北京时间显示。
const zone = 'Asia/Shanghai'
const clockFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: zone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
const fullFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: zone, year: 'numeric', month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
const dayKeyFormat = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit' })
const dayLabelFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: zone, month: 'long', day: 'numeric', weekday: 'short' })

/** 14:05 */
export const clock = (iso: string) => clockFormat.format(new Date(iso))
/** 2026年10月23日 14:05 */
export const fullTime = (iso: string) => fullFormat.format(new Date(iso))
/** 2026-10-23 */
export const dayKey = (iso: string) => dayKeyFormat.format(new Date(iso))
/** 10月23日周五 */
export const dayLabel = (iso: string, now = new Date()) => {
  const date = dayKey(iso)
  return `${date.slice(0, 4) === dayKeyFormat.format(now).slice(0, 4) ? '' : `${date.slice(0, 4)}年`}${dayLabelFormat.format(new Date(iso))}`
}

/** 日期或时间戳 → 10-23；非本年保留年份。 */
export const shortDate = (value: string, now = new Date()) => {
  const date = /^\d{4}-\d{2}-\d{2}$/.test(value) ? value : dayKey(value)
  return date.slice(0, 4) === dayKeyFormat.format(now).slice(0, 4) ? date.slice(5) : date
}

const todayFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: zone, year: 'numeric', month: 'long', day: 'numeric', weekday: 'long' })
/** 2026年10月25日星期日 */
export const todayLabel = (now = new Date()) => todayFormat.format(now)

// 按北京时间分日。列表已经按时间排好，这里只把相邻的同一天放在一组，保持原来的顺序。
export function groupByDay<T extends { published_at: string }>(items: T[], now = new Date()) {
  const groups: { key: string; label: string; items: T[] }[] = []
  for (const item of items) {
    const key = dayKey(item.published_at)
    const last = groups.at(-1)
    if (last?.key === key) last.items.push(item)
    else groups.push({ key, label: dayLabel(item.published_at, now), items: [item] })
  }
  return groups
}

// 日报日期本身就是北京时间的日期：按 UTC 解析再按 UTC 格式化，不受浏览器时区影响。
const reportDateFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: 'long', day: 'numeric', weekday: 'long' })
const weekdayFormat = new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', weekday: 'long' })
const reportDate = (date: string) => {
  const parsed = new Date(`${date}T00:00:00Z`)
  return /^\d{4}-\d{2}-\d{2}$/.test(date) && !Number.isNaN(parsed.getTime()) ? parsed : null
}
/** 2026-10-23 → 10月23日星期五；日期无效时原样返回。 */
export const reportDateLabel = (date: string, now = new Date()) => {
  const parsed = reportDate(date)
  if (!parsed) return date
  const year = date.slice(0, 4)
  return `${year === dayKeyFormat.format(now).slice(0, 4) ? '' : `${year}年`}${reportDateFormat.format(parsed)}`
}
/** 2026-10-23 → 星期五；日期无效时返回空字符串。 */
export const reportWeekday = (date: string) => { const parsed = reportDate(date); return parsed ? weekdayFormat.format(parsed) : '' }
