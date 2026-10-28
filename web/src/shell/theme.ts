import { useSyncExternalStore } from 'react'

// 存储键、取值和 theme-color 的改写规则与 public/theme.js 保持一致。
export type Theme = 'system' | 'light' | 'dark'
export const THEMES: { value: Theme; label: string }[] = [
  { value: 'system', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
]
const KEY = 'vane-theme'
const COLORS = { light: '#f6f5f0', dark: '#0f1012' }

function readSaved(): Theme {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved === 'light' || saved === 'dark') return saved
  } catch { /* 读不了存储时跟随系统。 */ }
  return 'system'
}

function apply(theme: Theme) {
  document.documentElement.dataset.theme = theme
  for (const meta of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
    const own = (meta.getAttribute('media') ?? '').includes('dark') ? 'dark' : 'light'
    meta.content = COLORS[theme === 'system' ? own : theme]
  }
}

let current = readSaved()
apply(current) // 主题脚本没加载成功时，这里补上。
const listeners = new Set<() => void>()

export function setTheme(theme: Theme) {
  current = theme
  apply(theme)
  try { localStorage.setItem(KEY, theme) } catch { /* 存储不可用时本次仍然切换。 */ }
  for (const listener of listeners) listener()
}

export function nextTheme(theme: Theme): Theme {
  return theme === 'system' ? 'light' : theme === 'light' ? 'dark' : 'system'
}

export const themeLabel = (theme: Theme) => THEMES.find(option => option.value === theme)!.label

export function useTheme() {
  return useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener) } }, () => current)
}
