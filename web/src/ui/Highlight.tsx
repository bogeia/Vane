import { Fragment } from 'react'
import { highlight } from '../lib/highlight'

// 搜索状态下标出命中的地方。q 为空时原样输出。
export function Highlight({ text, q }: { text: string; q: string }) {
  if (!q) return text
  return highlight(text, q).map((segment, index) => segment.hit ? <mark key={index}>{segment.text}</mark> : <Fragment key={index}>{segment.text}</Fragment>)
}
