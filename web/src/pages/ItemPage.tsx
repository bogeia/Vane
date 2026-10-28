import { useEffect } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import type { Item } from '../items/types'
import { useFavorite } from '../items/useFavorite'
import { useResource } from '../lib/resource'
import { tierLabel } from '../lib/tier'
import { fullTime } from '../lib/time'
import { useNavHighlight } from '../shell/Shell'
import { useReadingState } from '../useReadingState'
import { IconBookmark, IconChevron, IconExternal } from '../ui/icons'
import { Picture } from '../ui/Picture'
import { ErrorPage, SkeletonLines } from '../ui/States'
import '../items/items.css'
import './detail.css'

type Detail = Item & { related_items: Item[] }

// 资料详情 /items/:id。
export function ItemPage() {
  const { id = '' } = useParams()
  const { hash } = useLocation()
  const reading = useReadingState()
  const resource = useResource<Detail>(`/api/items/${encodeURIComponent(id)}`)
  const item = resource.status === 'ok' ? resource.data : null
  useNavHighlight(item ? (item.selected ? 'selected' : 'all') : null)
  useEffect(() => { if (item) reading.markRead([item.id]) }, [item, reading])
  // 从卡片上的“另外 N 家也报道了”进来时，加载完滚到“同一事件”。
  // 上面的配图加载完会把这一段往下推，所以读者自己操作之前（最多 5 秒），布局一变就重新对齐。
  useEffect(() => {
    const story = item && hash === '#story' ? document.getElementById('story') : null
    if (!story?.parentElement) return
    const align = () => story.scrollIntoView({ behavior: 'instant' })
    const observer = new ResizeObserver(align)
    observer.observe(story.parentElement)
    const stop = () => observer.disconnect()
    const events = ['wheel', 'touchstart', 'keydown', 'mousedown']
    for (const name of events) window.addEventListener(name, stop, { passive: true })
    const timer = setTimeout(stop, 5000)
    return () => {
      stop()
      clearTimeout(timer)
      for (const name of events) window.removeEventListener(name, stop)
    }
  }, [item, hash])

  if (resource.status === 'notfound' || resource.status === 'failed') return <ErrorPage kind={resource.status} what="资料" onRetry={resource.retry} />
  if (!item) return <div className="detail"><SkeletonLines /></div>
  return <ItemDetail item={item} />
}

function ItemDetail({ item }: { item: Detail }) {
  const reading = useReadingState()
  const favorite = useFavorite(item)
  const tier = tierLabel(item.source.tier)
  const list = item.selected ? { to: '/', label: '精选' } : { to: '/all', label: '全部动态' }
  return <article className="detail">
    <nav className="crumb" aria-label="位置"><Link to={list.to}>{list.label}</Link><IconChevron size={14} /><span aria-current="page">资料详情</span></nav>
    <div className="detail-meta">
      <span className="detail-source">{item.source.name}</span>
      {tier.short && <span className="tier" title={tier.full}>{tier.short}<span className="visually-hidden">（{tier.full}）</span></span>}
      <time dateTime={item.published_at}>{fullTime(item.published_at)}</time>
      {item.category && <span className="category-tag">{item.category}</span>}
      {item.selected && <span className="badge">精选</span>}
    </div>
    <h1>{item.title}</h1>
    {item.original_title && item.original_title !== item.title && <p className="detail-original">原标题：{item.original_title}</p>}
    <Picture className="detail-image" src={item.image_url} />
    <section className="detail-block" aria-labelledby="detail-summary">
      <h2 id="detail-summary">摘要</h2>
      <p>{item.summary}</p>
    </section>
    {item.reason && <section className="detail-block detail-reason" aria-labelledby="detail-reason">
      <h2 id="detail-reason">推荐理由</h2>
      <p>{item.reason}</p>
    </section>}
    {item.tags.length > 0 && <ul className="detail-tags" aria-label="标签">{item.tags.map(tag => <li key={tag}>{tag}</li>)}</ul>}
    <div className="detail-actions">
      <a className="button-primary" href={item.url} target="_blank" rel="noopener noreferrer" onClick={() => reading.markRead([item.id])} data-shortcut-original>
        <IconExternal size={17} />阅读原文<span className="visually-hidden">（新标签页）</span>
      </a>
      <button type="button" className="button-secondary" aria-pressed={favorite.saved} onClick={favorite.toggle} data-shortcut-favorite><IconBookmark size={17} />{favorite.saved ? '已收藏' : '收藏'}</button>
      {favorite.message && <span className="favorite-alert" role="alert">{favorite.message}</span>}
      <span className="detail-score"><b>{item.score}</b> 分</span>
    </div>
    <p className="detail-keys" aria-hidden="true"><kbd>o</kbd> 原文<kbd>f</kbd> 收藏<kbd>Esc</kbd> 返回<kbd>?</kbd> 全部快捷键</p>
    {item.story_id && <section id="story" className="detail-story" aria-labelledby="story-title">
      <h2 id="story-title">同一事件{item.related_items.length > 0 && <span>另外 {item.related_items.length} 家也报道了</span>}</h2>
      {item.related_items.length === 0
        ? <p className="detail-note">还没有其他信源报道这一事件。</p>
        : <ul>{item.related_items.map(other => <li key={other.id}>
          <Link to={`/items/${other.id}`}>{other.title}</Link>
          <p>{other.source.name} · {fullTime(other.published_at)} · {other.score} 分</p>
        </li>)}</ul>}
      <Link className="detail-more" to={`/stories/${item.story_id}`}>查看事件时间线<IconChevron size={14} /></Link>
    </section>}
  </article>
}
