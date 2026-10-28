import type { ReactNode } from 'react'
import { Link } from 'react-router'
import './states.css'

function Loading({ children }: { children: ReactNode }) {
  return <div className="skeleton" role="status">
    <span className="visually-hidden">正在加载…</span>
    <div aria-hidden="true">{children}</div>
  </div>
}

// 卡片列表：3 张骨架卡片。
export function SkeletonCards() {
  return <Loading>{[0, 1, 2].map(key => <div key={key} className="skeleton-card"><i /><i /><i /></div>)}</Loading>
}

// 紧凑行列表：6 行骨架。
export function SkeletonRows() {
  return <Loading><div className="skeleton-rows">{[0, 1, 2, 3, 4, 5].map(key => <div key={key} className="skeleton-row"><i /><i /></div>)}</div></Loading>
}

// 详情页：5 条骨架线。
export function SkeletonLines() {
  return <Loading><div className="skeleton-lines">{[0, 1, 2, 3, 4].map(key => <i key={key} />)}</div></Loading>
}

// 空状态：写原因和下一步。
export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>
}

export type ErrorKind = 'notfound' | 'failed'
export type ErrorSubject = '资料' | '日报' | '事件' | '主题' | '热点榜' | '页面'

// 统一错误页：整页的主数据找不到或加载失败时显示。
export function ErrorPage({ kind, what, onRetry }: { kind: ErrorKind; what: ErrorSubject; onRetry?: () => void }) {
  const failed = kind === 'failed'
  return <div className="error-page">
    <p className="error-code" aria-hidden="true">{failed ? '加载失败' : '404'}</p>
    <h1>{failed ? `${what}暂时加载不出来` : `这份${what}不存在，或者还没公开`}</h1>
    <p>{failed ? '可能是网络断了，或者服务正在重启。稍等一下再试。' : '链接可能写错了，也可能内容还在处理中。'}</p>
    <div className="error-actions">
      {failed && onRetry && <button type="button" className="button-primary" onClick={onRetry}>重试</button>}
      <Link className={failed && onRetry ? 'button-secondary' : 'button-primary'} to="/">回到精选</Link>
      <Link className="button-secondary" to="/all">看全部动态</Link>
    </div>
  </div>
}
