import { useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { groupByDay } from '../lib/time'
import { useResource } from '../lib/resource'
import { useReadingState } from '../useReadingState'
import { IconRss } from '../ui/icons'
import { Empty, ErrorPage, SkeletonCards, SkeletonRows } from '../ui/States'
import { ItemCard } from './ItemCard'
import { ItemRow } from './ItemRow'
import type { View } from './types'
import { useFeed } from './useFeed'
import './feed.css'

const CATEGORY_FEEDS: Record<string, string> = { 模型: 'model', 产品: 'product', 行业: 'industry', 论文: 'paper', 教程: 'tutorial', 观点: 'opinion' }

function feedLink(view: View, category: string) {
  if (view === 'all') return '/feed/all.xml'
  return CATEGORY_FEEDS[category] ? `/feed/category/${CATEGORY_FEEDS[category]}.xml` : '/feed.xml'
}

type FeedProps = {
  view: View
  head: ReactNode
  topic?: string
  q?: string
  // 搜索结果默认展开所有行。
  expandAll?: boolean
  // 主题页有自己的订阅链接，不显示这里的。
  rss?: boolean
}

// 资料列表：分类按钮、只看未读、RSS、按天分组的卡片（精选）或紧凑行（全部动态）、加载更多。
// 第一页加载失败时整页换成统一错误页，所以页面标题从 head 传进来。
export function Feed(props: FeedProps) {
  // 范围切换重置分类、展开与临时高度；搜索和分类沿用当前阅读位置。
  return <FeedContent key={JSON.stringify([props.view, props.topic ?? ''])} {...props} />
}

function FeedContent({ view, head, topic = '', q = '', expandAll = false, rss = true }: FeedProps) {
  const reading = useReadingState()
  const [category, setCategory] = useState('')
  const container = useRef<HTMLDivElement>(null)
  const [minHeight, setMinHeight] = useState(0)
  const meta = useResource<{ categories: string[] }>('/api/meta', { progress: false })
  const categories = meta.status === 'ok' ? meta.data.categories : []
  const feed = useFeed(view, topic, category, q)
  const [openId, setOpenId] = useState('')
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())

  const { status, more, loadMore } = feed

  if (status === 'failed') return <ErrorPage kind="failed" what="资料" onRetry={feed.retry} />

  const isOpen = (id: string) => expandAll ? !collapsed.has(id) : openId === id
  const toggle = (id: string) => {
    if (!expandAll) return setOpenId(openId === id ? '' : id)
    setCollapsed(previous => {
      const next = new Set(previous)
      if (!next.delete(id)) next.add(id)
      return next
    })
  }
  const emptyText: ReactNode = q ? <>没有找到包含「{q}」的资料。换个词，或者看<Link to="/all">全部动态</Link>。</>
    : feed.emptyBecauseRead ? '没有未读的资料了。关掉「只看未读」看全部。'
    : category ? '这个分类暂时没有资料。换个分类，或者看全部分类。'
    : topic ? '这个主题暂时没有资料。'
    : '还没有资料。信源会定时收集，稍后再来看看。'

  return <div ref={container} style={{ minHeight }}>
    {head}
    <div className="feed-tools">
      <div className="category-pills" role="group" aria-label="分类">
        {['', ...categories].map(value => <button type="button" key={value} aria-pressed={category === value} onClick={() => {
          if (value === category) return
          // 短列表也保留当前视口所需的高度，避免浏览器把滚动位置钳回顶部。
          setMinHeight(window.scrollY > 0 && container.current ? Math.max(0, window.innerHeight - container.current.getBoundingClientRect().top) : 0)
          setCategory(value)
        }}>{value || '全部分类'}</button>)}
      </div>
      <div className="feed-options">
        <label className="unread-toggle"><input type="checkbox" checked={reading.unreadOnly()} onChange={event => reading.setUnreadOnly(event.target.checked)} />只看未读</label>
        {rss && <a className="rss-link" href={feedLink(view, category)}><IconRss size={15} />订阅{view === 'selected' && CATEGORY_FEEDS[category] ? `${category}精选` : ''} RSS</a>}
      </div>
    </div>

    {feed.loadingEmpty && (view === 'selected' ? <SkeletonCards /> : <SkeletonRows />)}
    {feed.empty && <Empty>{emptyText}</Empty>}
    {groupByDay(feed.items).map(group => <section className="day" key={group.key} aria-labelledby={`day-${group.key}`}>
      <h2 className="day-head" id={`day-${group.key}`}>{group.label}<span> · {group.items.length} 条</span></h2>
      {view === 'selected'
        ? group.items.map(item => <ItemCard key={item.id} item={item} />)
        : <ol className="rows">{group.items.map(item => <ItemRow key={item.id} item={item} q={q} open={isOpen(item.id)} onToggle={() => toggle(item.id)} />)}</ol>}
    </section>)}

    {status === 'ready' && <div className="feed-foot">
      {more === 'failed' && <><p role="alert">加载更多失败，已加载的内容还在。</p><button type="button" className="button-secondary" onClick={loadMore}>重试</button></>}
      {more !== 'failed' && feed.canLoadMore && <button type="button" className="button-secondary" onClick={loadMore} disabled={more === 'loading'}>{more === 'loading' ? '正在加载…' : '加载更多'}</button>}
      {feed.finished && feed.items.length > 0 && <p>已加载全部</p>}
    </div>}
  </div>
}
