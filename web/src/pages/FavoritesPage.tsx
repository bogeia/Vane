import { Link } from 'react-router'
import '../items/items.css'
import type { Favorite } from '../readingState'
import { fullTime, shortDate } from '../lib/time'
import { useReadingState } from '../useReadingState'
import { Empty } from '../ui/States'
import '../ui/page.css'
import './topics.css'

// 收藏 /favorites：只读浏览器里的收藏快照，不请求资料接口，断网也能看。按收藏时间倒序。
export function FavoritesPage() {
  const reading = useReadingState()
  const favorites = reading.favorites()
  return <>
    <header className="page-head"><div><h1>收藏</h1><p>存在这个浏览器里，按收藏时间从新到旧。</p></div></header>
    {favorites.length === 0
      ? <Empty>还没有收藏。在资料卡片或详情页上点收藏，留着以后看。</Empty>
      : <ol className="rows favorite-rows">{favorites.map(item => <FavoriteRow key={item.id} item={item} />)}</ol>}
  </>
}

// 紧凑行的样式，但收藏快照里没有摘要，所以没有“摘要”按钮。
function FavoriteRow({ item }: { item: Favorite }) {
  const reading = useReadingState()
  return <li className={reading.isRead(item.id) ? 'row is-read' : 'row'} data-shortcut-item>
    <time dateTime={item.published_at} title={fullTime(item.published_at)}>{shortDate(item.published_at)}</time>
    <div className="row-body">
      <p className="row-line"><Link className="row-title" to={`/items/${encodeURIComponent(item.id)}`} data-shortcut-title>{item.title}</Link></p>
      <p className="row-sub">
        <span>{item.source_name}</span>
        <a href={item.url} target="_blank" rel="noopener noreferrer" onClick={() => reading.markRead([item.id])} data-shortcut-original>原文 ↗<span className="visually-hidden">（新标签页）</span></a>
        <button type="button" onClick={() => reading.removeFavorite(item.id)} data-shortcut-favorite>取消收藏</button>
      </p>
    </div>
  </li>
}
