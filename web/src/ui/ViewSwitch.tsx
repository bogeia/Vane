import { Link } from 'react-router'
import './view-switch.css'

// 精选与全部范围切换，在所有屏幕上保留当前位置。
export function ViewSwitch({ view }: { view: 'selected' | 'all' }) {
  return <nav className="view-switch" aria-label="切换列表">
    <Link to="/" aria-current={view === 'selected' ? 'page' : undefined}>精选</Link>
    <Link to="/all" aria-current={view === 'all' ? 'page' : undefined}>全部</Link>
  </nav>
}
