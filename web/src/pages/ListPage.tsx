import { Link, useSearchParams } from 'react-router'
import { Feed } from '../items/Feed'
import type { View } from '../items/types'
import { normalizeQuery } from '../lib/search'
import { todayLabel } from '../lib/time'
import { Columns } from '../shell/Shell'
import { HomeRail } from '../ui/HomeRail'
import { ViewSwitch } from '../ui/ViewSwitch'
import '../ui/page.css'

const SUBTITLES: Record<View, string> = {
  selected: '评分过线的资料，同一事件只出现一次',
  all: '所有信源的资料，按发布时间排列，点「摘要」展开',
}

// 精选 / 和全部动态 /all。全部动态的地址里有 q 时是搜索状态，结果行默认展开。
export function ListPage({ view }: { view: View }) {
  const [params] = useSearchParams()
  const q = view === 'all' ? normalizeQuery(params.get('q')) : ''
  const head = <header className="page-head">
    <div>
      {q
        ? <><h1>搜索「{q}」</h1><p><Link className="clear-search" to="/all">清除搜索</Link></p></>
        : <><h1>{view === 'selected' ? '精选' : '全部动态'}</h1><p>{todayLabel()} · {SUBTITLES[view]}</p></>}
    </div>
    <ViewSwitch view={view} />
  </header>
  return <Columns rail={<HomeRail />}><Feed view={view} q={q} expandAll={q !== ''} head={head} /></Columns>
}
