export type Release = { id: string; date: string; title: string; items: string[] }

// 校验手写的更新日志：不为空，id 不重复，日期是 YYYY-MM-DD，标题不为空，至少一条改动，按日期从新到旧。
// 返回发现的问题，空数组表示通过。
export function validateReleases(data: unknown): string[] {
  const releases = (data as { releases?: unknown } | null)?.releases
  if (!Array.isArray(releases) || releases.length === 0) return ['releases 为空']
  const problems: string[] = []
  const ids = new Set<string>()
  let previous = ''
  releases.forEach((release: Partial<Release>, index) => {
    const at = `第 ${index + 1} 条`
    if (typeof release.id !== 'string' || release.id === '') problems.push(`${at} 缺少 id`)
    else if (ids.has(release.id)) problems.push(`${at} 的 id ${release.id} 重复`)
    else ids.add(release.id)
    const date = typeof release.date === 'string' ? release.date : ''
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || Number.isNaN(Date.parse(`${date}T00:00:00Z`))) problems.push(`${at} 的日期不是 YYYY-MM-DD`)
    else if (previous && date > previous) problems.push(`${at} 的日期比上一条新，没有按从新到旧排列`)
    else previous = date
    if (typeof release.title !== 'string' || release.title.trim() === '') problems.push(`${at} 缺少标题`)
    if (!Array.isArray(release.items) || release.items.length === 0 || release.items.some(item => typeof item !== 'string' || item.trim() === '')) problems.push(`${at} 至少要有一条改动`)
  })
  return problems
}

type Storage = { getItem(key: string): string | null; setItem(key: string, value: string): void }
const KEY = 'vane-changelog-seen'

// 更新日志红点：记着看过的最新一条的 id。最新一条和记录不同、或者没有记录时显示红点；读不了存储时不显示。
export function createChangelogSeen(latestId: string, storage: () => Storage) {
  let seen: string | null = null
  let readable = true
  try { seen = storage().getItem(KEY) } catch { readable = false }
  let revision = 0
  const listeners = new Set<() => void>()
  return {
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
    version: () => revision,
    unseen: () => readable && seen !== latestId,
    markSeen() {
      if (seen === latestId) return
      seen = latestId
      try { storage().setItem(KEY, latestId) } catch { /* 这次打开期间仍然不显示红点。 */ }
      revision++
      for (const listener of listeners) listener()
    },
  }
}
