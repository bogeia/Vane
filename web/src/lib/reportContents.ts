import type { Item } from '../items/types'

export type Report = { date: string; headline: Item | null; highlights: Item[]; sections: { category: string; items: Item[] }[]; briefs: Item[] }

export const reportItemAnchor = (id: string) => `r-item-${encodeURIComponent(id)}`

// 分类正文决定阅读次序；其余入口只补充独有资料，保留同一资料的不同摘要和理由。
export function reportContents(report: Report) {
  const items = new Map<string, Item>()
  const groups = report.sections.map(section => ({ ...section, items: [] as Item[] }))
  const extra: Item[] = []
  const briefs: Item[] = []
  const add = (item: Item, target: Item[]) => {
    const existing = items.get(item.id)
    if (existing) {
      for (const field of ['summary', 'reason'] as const) {
        if (item[field] && !existing[field].includes(item[field])) existing[field] = [existing[field], item[field]].filter(Boolean).join('\n\n')
      }
      return
    }
    const copy = { ...item }
    items.set(item.id, copy)
    target.push(copy)
  }
  report.sections.forEach((section, index) => section.items.forEach(item => add(item, groups[index].items)))
  if (report.headline) add(report.headline, extra)
  report.highlights.forEach(item => add(item, extra))
  report.briefs.forEach(item => add(item, briefs))
  return { sections: [...(extra.length ? [{ category: '本期重点', items: extra }] : []), ...groups].filter(section => section.items.length), briefs, count: items.size }
}
