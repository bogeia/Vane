// 外壳的导航规则：侧栏和手机底部标签在哪些地址高亮，哪些地址按详情页处理。
export type NavKey = 'selected' | 'all' | 'hot' | 'reports' | 'topics' | 'favorites' | 'about' | 'agent' | 'changelog'
export type TabKey = 'selected' | 'hot' | 'reports' | 'more'

// 资料详情由页面按资料是否入选决定高亮，这里返回 null。
export function navKeyFor(pathname: string): NavKey | null {
  if (pathname === '/') return 'selected'
  if (pathname === '/all') return 'all'
  if (pathname === '/hot' || pathname.startsWith('/stories/')) return 'hot'
  if (pathname === '/reports' || pathname.startsWith('/reports/')) return 'reports'
  if (pathname === '/topics' || pathname.startsWith('/topics/')) return 'topics'
  if (pathname === '/favorites') return 'favorites'
  if (pathname === '/about') return 'about'
  if (pathname === '/agent') return 'agent'
  if (pathname === '/changelog') return 'changelog'
  return null
}

export function tabKeyFor(pathname: string, nav: NavKey | null): TabKey | null {
  if (pathname === '/more') return 'more'
  switch (nav) {
    case 'selected': case 'all': return 'selected'
    case 'hot': return 'hot'
    case 'reports': return 'reports'
    case 'topics': case 'favorites': case 'about': case 'agent': case 'changelog': return 'more'
    default: return null
  }
}

// 资料详情和事件页：手机顶栏显示“‹ 返回”，隐藏底部标签栏。
export const isDetailPath = (pathname: string) => /^\/(items|stories)\/[^/]+$/.test(pathname)
