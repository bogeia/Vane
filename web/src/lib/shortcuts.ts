export type ShortcutPage = 'list' | 'detail' | 'other'
export type ShortcutAction = 'focus-search' | 'next' | 'prev' | 'open-original' | 'favorite' | 'toggle-help' | 'close-help' | 'close-search' | 'back'

// 列表页：精选、全部动态、主题页、收藏、热点榜、日报详情、事件页。详情页：资料详情。其他页面只响应 ? 和 Esc。
export function shortcutPage(pathname: string): ShortcutPage {
  if (/^\/items\/[^/]+$/.test(pathname)) return 'detail'
  if (['/', '/all', '/favorites', '/hot'].includes(pathname) || /^\/(topics|reports|stories)\/[^/]+$/.test(pathname)) return 'list'
  return 'other'
}

export type KeyInput = {
  key: string
  ctrl: boolean
  meta: boolean
  alt: boolean
  // 焦点在 input、textarea、select 或可编辑元素里。
  editable: boolean
  page: ShortcutPage
  helpOpen: boolean
  // 手机外壳下展开的搜索框。
  searchOpen: boolean
}

// 判断一次按键要做什么，不做就返回 null。页面只负责执行。
export function shortcutAction({ key, ctrl, meta, alt, editable, page, helpOpen, searchOpen }: KeyInput): ShortcutAction | null {
  if (ctrl || meta || alt) return null
  if (key === 'Escape') {
    if (helpOpen) return 'close-help'
    if (searchOpen) return 'close-search'
    return page === 'detail' && !editable ? 'back' : null
  }
  if (editable) return null
  if (key === '?') return 'toggle-help'
  if (helpOpen || page === 'other') return null
  switch (key) {
    case '/': return 'focus-search'
    case 'o': return 'open-original'
    case 'f': return 'favorite'
    case 'j': return page === 'list' ? 'next' : null
    case 'k': return page === 'list' ? 'prev' : null
    default: return null
  }
}

// 当前条移动：current 是当前条的位置，没有当前条（或当前条已经不在列表里）时是 -1 或越界。
// 没有当前条时 j 选第一条、k 选最后一条；到头了停住，不循环。列表为空时返回 -1。
export function nextIndex(current: number, count: number, direction: 1 | -1): number {
  if (count <= 0) return -1
  if (current < 0 || current >= count) return direction > 0 ? 0 : count - 1
  return Math.min(count - 1, Math.max(0, current + direction))
}
