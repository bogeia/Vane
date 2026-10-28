// 搜索词去掉首尾空白，最多 200 个字，和接口的限制一致；地址里更长的截掉。
export const normalizeQuery = (raw: string | null) => [...(raw ?? '').trim()].slice(0, 200).join('')

// 搜索结果在全部动态里，搜索词写进地址。空搜索词等于清除搜索。
export const searchPath = (q: string) => q ? `/all?q=${encodeURIComponent(q)}` : '/all'
