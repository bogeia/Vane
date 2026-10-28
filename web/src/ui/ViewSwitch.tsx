import { Link } from 'react-router'
import './view-switch.css'

// 精选/全部切换：两个链接。桌面用侧栏切换，所以只在手机外壳里显示。
export function ViewSwitch({ view }: { view: 'selected' | 'all' }) {
  return <nav className="view-switch" aria-label="切换列表">
    <Link to="/" aria-current={view === 'selected' ? 'page' : undefined}>精选</Link>
    <Link to="/all" aria-current={view === 'all' ? 'page' : undefined}>全部</Link>
  </nav>
}
