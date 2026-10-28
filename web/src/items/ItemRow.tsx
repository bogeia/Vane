import { Link } from 'react-router'
import { clock } from '../lib/time'
import { useReadingState } from '../useReadingState'
import { Highlight } from '../ui/Highlight'
import type { Item } from './types'
import { useFavorite } from './useFavorite'
import './items.css'

// 紧凑行：一行一条，点“摘要”展开摘要和推荐理由。
// q 是搜索词：标题、原标题和摘要里的命中高亮，原标题和标题不一样时显示在标题下面。
export function ItemRow({ item, open, onToggle, q = '' }: { item: Item; open: boolean; onToggle: () => void; q?: string }) {
  const reading = useReadingState()
  const favorite = useFavorite(item)
  const detailId = `row-detail-${item.id}`
  return <li className={`row${open ? ' is-open' : ''}${reading.isRead(item.id) ? ' is-read' : ''}`} data-shortcut-item>
    <time dateTime={item.published_at}>{clock(item.published_at)}</time>
    <div className="row-body">
      <p className="row-line">
        <Link className="row-title" to={`/items/${item.id}`} data-shortcut-title><Highlight text={item.title} q={q} /></Link>
        {item.selected && <span className="badge">精选</span>}
        {item.story_others > 0 && <span className="cluster" title={`另外 ${item.story_others} 家也报道了`}>
          <span aria-hidden="true">+{item.story_others}</span><span className="visually-hidden">另外 {item.story_others} 家也报道了</span>
        </span>}
      </p>
      {q && item.original_title && item.original_title !== item.title && <p className="row-original">原标题：<Highlight text={item.original_title} q={q} /></p>}
      <p className="row-sub">
        <span>{item.source.name}</span>
        {item.category && <span>{item.category}</span>}
        <span>{item.score} 分</span>
        <button type="button" aria-expanded={open} aria-controls={detailId} onClick={onToggle}>{open ? '收起' : '摘要'}</button>
        <button type="button" onClick={favorite.toggle} data-shortcut-favorite>{favorite.saved ? '已收藏' : '收藏'}</button>
        <a href={item.url} target="_blank" rel="noopener noreferrer" onClick={() => reading.markRead([item.id])} data-shortcut-original>原文 ↗<span className="visually-hidden">（新标签页）</span></a>
        {favorite.message && <span className="favorite-alert" role="alert">{favorite.message}</span>}
      </p>
      {open && <div className="row-detail" id={detailId}>
        <p><Highlight text={item.summary} q={q} /></p>
        {item.reason && <p className="row-reason">{item.reason}</p>}
      </div>}
    </div>
  </li>
}
