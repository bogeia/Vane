import type { ReactNode } from 'react'
import './page.css'

export function TableOfContents({ title = '本页目录', children }: { title?: string; children: ReactNode }) {
  return <nav className="panel" aria-label={title}>
    <div className="panel-head"><h2>{title}</h2></div>
    <ol className="toc">{children}</ol>
  </nav>
}
