// 信源分级的中文写法：页面上显示短名，悬停和关于页用完整说明。未知代号原样显示。
const TIERS: Record<string, { short: string; full: string }> = {
  official: { short: '官方', full: '官方一手' },
  media: { short: '媒体', full: '媒体与个人' },
}

export function tierLabel(code: string) {
  return Object.hasOwn(TIERS, code) ? TIERS[code] : { short: code, full: code }
}

// 信源列表 OPML 的分组标题是完整说明，反查短名；查不到时返回 null。
export function tierOfGroup(title: string) {
  return Object.values(TIERS).find(tier => tier.full === title) ?? null
}
