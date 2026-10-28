export type FavoriteItem = { id: string; title: string; source: { name: string }; published_at: string; url: string }
export type Favorite = { id: string; title: string; source_name: string; published_at: string; url: string; favorited_at: number }
function isFavorite(value: unknown): value is Favorite {
  if (!value || typeof value !== 'object') return false
  const item = value as Favorite
  return typeof item.id === 'string' && item.id !== '' && typeof item.title === 'string' && typeof item.source_name === 'string' && typeof item.published_at === 'string' && Number.isFinite(Date.parse(item.published_at)) && typeof item.url === 'string' && /^https?:\/\//i.test(item.url) && typeof item.favorited_at === 'number' && Number.isFinite(item.favorited_at)
}

type Storage = { getItem(key: string): string | null; setItem(key: string, value: string): void }

export function createReadingState(storage: () => Storage = () => localStorage, now: () => number = Date.now) {
  let saved: Favorite[] = []
  try {
    const parsed: unknown = JSON.parse(storage().getItem('vane-favorites') ?? '[]')
    if (Array.isArray(parsed)) {
      const unique = new Map<string, Favorite>()
      for (const item of parsed) if (isFavorite(item) && !unique.has(item.id)) unique.set(item.id, item)
      saved = [...unique.values()].slice(0, 1000)
    }
  } catch { /* 存储不可用时使用内存。 */ }
  let read = new Map<string, number>()
  try {
    const parsed: unknown = JSON.parse(storage().getItem('vane-read') ?? '[]')
    if (Array.isArray(parsed)) {
      for (const entry of parsed) {
        if (entry && typeof entry.id === 'string' && typeof entry.read_at === 'number' && Number.isFinite(entry.read_at)) read.set(entry.id, entry.read_at)
      }
      read = new Map([...read].sort((a, b) => a[1] - b[1]).slice(-5000))
    }
  } catch { /* 存储不可用时使用内存。 */ }
  let unreadOnly = false
  try { unreadOnly = storage().getItem('vane-unread-only') === 'true' } catch { /* 默认显示全部。 */ }
  let revision = 0
  const listeners = new Set<() => void>()
  function persist() {
    try { storage().setItem('vane-favorites', JSON.stringify(saved)) } catch { /* 内存中的收藏继续可用。 */ }
    revision++
    for (const listener of listeners) listener()
  }
  return {
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
    version: () => revision,
    isRead: (id: string) => read.has(id),
    unreadOnly: () => unreadOnly,
    setUnreadOnly(value: boolean) {
      unreadOnly = value
      try { storage().setItem('vane-unread-only', String(value)) } catch { /* 当前会话继续使用开关。 */ }
      revision++
      for (const listener of listeners) listener()
    },
    isVisible(item: { id: string; story_item_ids?: string[] }, view: 'selected' | 'all') {
      const ids = view === 'selected' && item.story_item_ids?.length ? item.story_item_ids : [item.id]
      return !unreadOnly || !ids.every(id => read.has(id))
    },
    markRead(ids: string[]) {
      for (const id of ids) { read.delete(id); read.set(id, now()) }
      read = new Map([...read].sort((a, b) => a[1] - b[1]).slice(-5000))
      try { storage().setItem('vane-read', JSON.stringify([...read].map(([id, read_at]) => ({ id, read_at })))) } catch { /* 当前会话继续记录。 */ }
      revision++
      for (const listener of listeners) listener()
    },
    favorites: () => saved.map(value => ({ ...value })).sort((a, b) => b.favorited_at - a.favorited_at),
    isFavorite: (id: string) => saved.some(value => value.id === id),
    addFavorite(item: FavoriteItem) {
      if (saved.some(value => value.id === item.id)) return 'exists' as const
      if (saved.length >= 1000) return 'full' as const
      saved.push({ id: item.id, title: item.title, source_name: item.source.name, published_at: item.published_at, url: item.url, favorited_at: now() })
      persist()
      return 'added' as const
    },
    removeFavorite(id: string) {
      const previous = saved.length
      saved = saved.filter(value => value.id !== id)
      if (saved.length === previous) return false
      persist()
      return true
    },
  }
}
