import type { ReactNode } from 'react'

export type Icon = (props: { size?: number }) => ReactNode

const icon = (paths: ReactNode): Icon => ({ size = 20 }) =>
  <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">{paths}</svg>

// 风向标：箭头、尾翼、立杆、横档、底座。
export function BrandMark({ size = 24 }: { size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
    <path d="M3.5 8.5h13" />
    <path d="M16 5l4.5 3.5L16 12z" fill="currentColor" />
    <path d="M3.5 8.5L2 5.5M3.5 8.5L2 11.5M6.5 8.5L5 5.5M6.5 8.5L5 11.5" />
    <path d="M11 8.5V20.5M8 20.5h6" />
    <path d="M8.5 15.5h5" />
  </svg>
}

export const IconBolt = icon(<path d="M13 2L4 14h7l-1 8 9-12h-7l1-8z" />)
export const IconFlame = icon(<path d="M12 22c4 0 7-2.8 7-7 0-3.5-2.2-5.8-4-8-.3 2-1.3 3.3-2.5 4C12.8 7.5 11 4.5 8.5 2.5 8.7 6 5 8.5 5 14c0 4.2 3 8 7 8z" />)
export const IconDoc = icon(<><path d="M6 3h9l4 4v14H6z" /><path d="M14 3v5h5M9 13h7M9 17h7" /></>)
export const IconUser = icon(<><circle cx="12" cy="8" r="4" /><path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6" /></>)
export const IconList = icon(<path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01" />)
export const IconGrid = icon(<><rect x="4" y="4" width="7" height="7" rx="1.5" /><rect x="13" y="4" width="7" height="7" rx="1.5" /><rect x="4" y="13" width="7" height="7" rx="1.5" /><rect x="13" y="13" width="7" height="7" rx="1.5" /></>)
export const IconBookmark = icon(<path d="M6 3h12v18l-6-4-6 4z" />)
export const IconSearch = icon(<><circle cx="11" cy="11" r="7" /><path d="M20 20l-3.5-3.5" /></>)
export const IconSun = icon(<><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></>)
export const IconMoon = icon(<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z" />)
export const IconContrast = icon(<><circle cx="12" cy="12" r="9" /><path d="M12 3v18" /><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" /></>)
export const IconInfo = icon(<><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7.5h.01" /></>)
export const IconHistory = icon(<><path d="M3 12a9 9 0 1 0 3-6.7L3 8" /><path d="M3 3v5h5M12 7v5l3 2" /></>)
export const IconArrowUp = icon(<path d="M12 19V5M6 11l6-6 6 6" />)
export const IconExternal = icon(<path d="M14 4h6v6M20 4l-9 9M18 14v6H4V6h6" />)
export const IconRss = icon(<><path d="M5 5a14 14 0 0 1 14 14M5 11a8 8 0 0 1 8 8" /><circle cx="6" cy="18" r="1.5" fill="currentColor" /></>)
export const IconChevron = icon(<path d="M9 6l6 6-6 6" />)
