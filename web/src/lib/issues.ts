// 日报列表按日期从新到旧。给定当前这一期，找出相邻的更早一期和更新一期；当前日期不在列表里时两边都没有。
export function adjacentIssues(dates: string[], current: string): { older?: string; newer?: string } {
  const index = dates.indexOf(current)
  if (index === -1) return {}
  return { older: dates[index + 1], newer: index > 0 ? dates[index - 1] : undefined }
}
