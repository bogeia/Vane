export type Segment = { text: string; hit: boolean }

// 把搜索词当作一段字面文本在原文里找，英文不区分大小写，标出每一处命中，规则和后端的 ILIKE 一致。
// 有些字符转成小写后长度会变（比如 İ），位置对不上，这时不高亮。
export function highlight(text: string, query: string): Segment[] {
  const plain = [{ text, hit: false }]
  if (!query || !text) return plain
  const lowerText = text.toLowerCase()
  const lowerQuery = query.toLowerCase()
  if (lowerText.length !== text.length || lowerQuery.length !== query.length) return plain
  const segments: Segment[] = []
  let start = 0
  for (let at = lowerText.indexOf(lowerQuery); at !== -1; at = lowerText.indexOf(lowerQuery, start)) {
    if (at > start) segments.push({ text: text.slice(start, at), hit: false })
    segments.push({ text: text.slice(at, at + query.length), hit: true })
    start = at + query.length
  }
  if (segments.length === 0) return plain
  if (start < text.length) segments.push({ text: text.slice(start), hit: false })
  return segments
}
