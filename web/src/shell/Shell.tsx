import { createContext, useContext, useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react'
import { Link, Outlet, useLocation, useNavigate, useNavigationType } from 'react-router'
import { useChangelogUnseen } from '../changelogState'
import { useMediaQuery } from '../lib/media'
import { useProgressActive } from '../lib/resource'
import { refreshReading, useReports } from '../lib/reading'
import { normalizeQuery, searchPath } from '../lib/search'
import { nextIndex, shortcutAction, shortcutPage, type ShortcutAction } from '../lib/shortcuts'
import {
  BrandMark, IconArrowUp, IconBolt, IconBookmark, IconContrast, IconDoc, IconFlame, IconGrid, IconHistory, IconInfo, IconList,
  IconMoon, IconRss, IconSearch, IconSun, IconUser, type Icon,
} from '../ui/icons'
import { isDetailPath, navKeyFor, tabKeyFor, type NavKey, type TabKey } from './nav'
import { nextTheme, setTheme, themeLabel, useTheme, type Theme } from './theme'
import './shell.css'

const NAV_GROUPS: { title: string; items: { key: NavKey; label: string; to: string; icon: Icon }[] }[] = [
  { title: '内容', items: [
    { key: 'selected', label: '精选', to: '/', icon: IconBolt },
    { key: 'all', label: '全部动态', to: '/all', icon: IconList },
    { key: 'hot', label: '热点榜', to: '/hot', icon: IconFlame },
    { key: 'reports', label: '日报', to: '/reports', icon: IconDoc },
    { key: 'topics', label: '主题', to: '/topics', icon: IconGrid },
    { key: 'favorites', label: '收藏', to: '/favorites', icon: IconBookmark },
  ] },
  { title: '更多', items: [
    { key: 'about', label: '关于', to: '/about', icon: IconInfo },
    { key: 'agent', label: '接入 Agent', to: '/agent', icon: IconRss },
    { key: 'changelog', label: '更新日志', to: '/changelog', icon: IconHistory },
  ] },
]

const THEME_ICONS: Record<Theme, Icon> = { system: IconContrast, light: IconSun, dark: IconMoon }

// 资料详情按资料是否入选决定侧栏高亮，由页面告诉外壳。
const NavHighlight = createContext<(key: NavKey | null) => void>(() => {})

export function useNavHighlight(key: NavKey | null) {
  const set = useContext(NavHighlight)
  useEffect(() => {
    set(key)
    return () => set(null)
  }, [key, set])
}

// 右栏插槽：宽度 ≥1181 才渲染，更窄时不显示，内容也不挪到别处。
export function Columns({ rail, children }: { rail: ReactNode; children: ReactNode }) {
  const wide = useMediaQuery('(min-width: 1181px)')
  return <div className={wide ? 'columns has-rail' : 'columns'}>
    <div className="columns-main">{children}</div>
    {wide && <aside className="columns-rail" aria-label="补充信息">{rail}</aside>}
  </div>
}

export function Shell() {
  const { pathname, search, hash } = useLocation()
  const navigationType = useNavigationType()
  const navigate = useNavigate()
  const theme = useTheme()
  const changelogUnseen = useChangelogUnseen()
  const [override, setOverride] = useState<NavKey | null>(null)
  const nav = override ?? navKeyFor(pathname)
  const tab = tabKeyFor(pathname, nav)
  const detail = isDetailPath(pathname)
  const reports = useReports({ progress: false })
  useEffect(() => refreshReading(), [pathname, search])
  const latestReport = reports.status === 'ok' ? reports.data.items[0]?.date : undefined
  const [searchOpen, setSearchOpen] = useState(false)
  const searchButton = useRef<HTMLButtonElement>(null)
  const closeSearch = () => { setSearchOpen(false); searchButton.current?.focus() }

  // 换页时收起手机搜索框。
  useEffect(() => setSearchOpen(false), [pathname, search])

  const help = useRef<HTMLDialogElement>(null)
  const helpReturn = useRef<HTMLElement | null>(null)
  const openHelp = () => {
    if (!help.current || help.current.open) return
    helpReturn.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    help.current.showModal()
  }

  // 点链接进入新页面时回到顶部；后退、前进交给浏览器。
  useEffect(() => {
    if (navigationType !== 'POP' && !hash) window.scrollTo({ top: 0, behavior: 'instant' })
  }, [pathname, navigationType, hash])

  const ThemeIcon = THEME_ICONS[theme]
  const cycleTheme = () => setTheme(nextTheme(theme))
  const goBack = () => (window.history.state?.idx ?? 0) > 0 ? navigate(-1) : navigate('/')

  // 快捷键：判断交给 shortcutAction，这里只执行。
  const run = (action: ShortcutAction) => {
    const main = document.getElementById('main')
    const items = [...(main?.querySelectorAll<HTMLElement>('[data-shortcut-item]') ?? [])]
    const current = items.find(item => item.contains(document.activeElement))
    const scope = shortcutPage(pathname) === 'detail' ? main : current
    switch (action) {
      case 'focus-search':
        if (matchMedia('(max-width: 960px)').matches) setSearchOpen(true)
        else document.querySelector<HTMLInputElement>('.sidebar-search input')?.focus()
        return
      case 'next': case 'prev': {
        const target = items[nextIndex(current ? items.indexOf(current) : -1, items.length, action === 'next' ? 1 : -1)]
        target?.querySelector<HTMLElement>('[data-shortcut-title]')?.focus()
        return
      }
      case 'open-original': scope?.querySelector<HTMLElement>('[data-shortcut-original]')?.click(); return
      case 'favorite': scope?.querySelector<HTMLElement>('[data-shortcut-favorite]')?.click(); return
      case 'toggle-help': if (help.current?.open) help.current.close(); else openHelp(); return
      case 'close-help': help.current?.close(); return
      case 'close-search': closeSearch(); return
      case 'back': goBack(); return
    }
  }
  const runRef = useRef(run)
  runRef.current = run
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.isComposing) return
      const target = event.target instanceof HTMLElement ? event.target : null
      const action = shortcutAction({
        key: event.key, ctrl: event.ctrlKey, meta: event.metaKey, alt: event.altKey,
        editable: !!target && (target.isContentEditable || target.matches('input, textarea, select')),
        page: shortcutPage(location.pathname), helpOpen: help.current?.open ?? false,
        searchOpen: document.getElementById('phone-search') !== null,
      })
      if (!action) return
      event.preventDefault()
      runRef.current(action)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
  const tabs: { key: TabKey; label: string; to: string; icon: Icon }[] = [
    { key: 'selected', label: '精选', to: '/', icon: IconBolt },
    { key: 'hot', label: '热点', to: '/hot', icon: IconFlame },
    { key: 'reports', label: '日报', to: latestReport ? `/reports/${latestReport}` : '/reports', icon: IconDoc },
    { key: 'more', label: '我的', to: '/more', icon: IconUser },
  ]

  return <NavHighlight.Provider value={setOverride}>
    <div className={detail ? 'shell is-detail' : 'shell'}>
      <a className="skip-link" href="#main" onClick={skipToMain}>跳到正文</a>
      <ProgressBar />

      <aside className="sidebar">
        <Link className="brand" to="/"><span className="brand-mark"><BrandMark size={22} /></span><span>Vane</span></Link>
        <SearchForm className="sidebar-search" />
        <nav aria-label="主导航">
          {NAV_GROUPS.map(group => <div className="nav-group" key={group.title}>
            <p className="nav-title" id={`nav-${group.title}`}>{group.title}</p>
            <ul aria-labelledby={`nav-${group.title}`}>{group.items.map(item => <li key={item.key}>
              <Link to={item.to} aria-current={item.key === nav ? 'page' : undefined}><item.icon size={18} /><span>{item.label}</span>
                {item.key === 'changelog' && changelogUnseen && <NewDot />}</Link>
            </li>)}</ul>
          </div>)}
        </nav>
        <div className="sidebar-foot">
          <button type="button" className="sidebar-theme" onClick={cycleTheme}><ThemeIcon size={16} />外观：{themeLabel(theme)}</button>
          <button type="button" className="sidebar-help" onClick={openHelp} aria-label="全部快捷键"><kbd>?</kbd></button>
        </div>
      </aside>

      <header className="phonebar">
        {detail
          ? <button type="button" className="phonebar-back" onClick={goBack}><span aria-hidden="true">‹</span>返回</button>
          : <Link className="brand" to="/"><span className="brand-mark"><BrandMark size={20} /></span><span>Vane</span></Link>}
        <button type="button" className="phonebar-icon" aria-label="搜索资料" aria-expanded={searchOpen} aria-controls="phone-search" ref={searchButton}
          onClick={() => searchOpen ? closeSearch() : setSearchOpen(true)}><IconSearch /></button>
        <button type="button" className="phonebar-icon" aria-label={`外观：${themeLabel(theme)}，点按切换`} onClick={cycleTheme}><ThemeIcon /></button>
      </header>
      {searchOpen && <SearchForm id="phone-search" className="phone-search" autoFocus onClose={closeSearch} />}

      <main id="main" className="shell-main" tabIndex={-1}>
        <div className="shell-content"><Outlet /></div>
      </main>

      {!detail && <nav className="tabbar" aria-label="底部导航">
        {tabs.map(item => <Link key={item.key} to={item.to} aria-current={item.key === tab ? 'page' : undefined}>
          <item.icon size={22} /><span>{item.label}</span>{item.key === 'more' && changelogUnseen && <NewDot />}
        </Link>)}
      </nav>}

      <BackToTop key={pathname} />

      <dialog ref={help} className="help" aria-labelledby="help-title" onClose={() => helpReturn.current?.focus()}>
        <div className="help-head"><h2 id="help-title">快捷键</h2><button type="button" className="help-close" onClick={() => help.current?.close()} aria-label="关闭">×</button></div>
        <dl>
          <div><dt><kbd>/</kbd></dt><dd>搜索资料</dd></div>
          <div><dt><kbd>j</kbd> <kbd>k</kbd></dt><dd>列表里的下一条、上一条</dd></div>
          <div><dt><kbd>Enter</kbd></dt><dd>打开当前这条</dd></div>
          <div><dt><kbd>o</kbd></dt><dd>在新标签页打开原文</dd></div>
          <div><dt><kbd>f</kbd></dt><dd>收藏或取消收藏</dd></div>
          <div><dt><kbd>Esc</kbd></dt><dd>关掉帮助或搜索框；在资料详情返回</dd></div>
          <div><dt><kbd>?</kbd></dt><dd>打开或关闭这个帮助</dd></div>
        </dl>
        <p>焦点在输入框里，或者按着 Ctrl、⌘、Alt 时，快捷键不生效。</p>
      </dialog>
    </div>
  </NavHighlight.Provider>
}

// 搜索框：提交后去 /all?q=词。在全部动态里时显示当前的搜索词。
function SearchForm({ id, className, autoFocus = false, onClose }: { id?: string; className: string; autoFocus?: boolean; onClose?: () => void }) {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  const current = pathname === '/all' ? normalizeQuery(new URLSearchParams(search).get('q')) : ''
  const [draft, setDraft] = useState(current)
  useEffect(() => setDraft(current), [current])
  return <form id={id} className={className} role="search" onSubmit={event => {
    event.preventDefault()
    onClose?.()
    navigate(searchPath(normalizeQuery(draft)))
  }}>
    <IconSearch size={16} />
    <input type="search" name="q" value={draft} maxLength={200} placeholder="搜索资料" aria-label="搜索资料" enterKeyHint="search" autoFocus={autoFocus}
      onChange={event => setDraft(event.target.value)} />
    {!onClose && <kbd aria-hidden="true">/</kbd>}
  </form>
}

// 更新日志有新内容时的红点。
export function NewDot() {
  return <i className="new-dot"><span className="visually-hidden">（有新的更新）</span></i>
}

function skipToMain(event: MouseEvent) {
  const main = document.getElementById('main')
  if (!main) return
  event.preventDefault() // 不把 #main 写进地址。
  main.focus()
}

function ProgressBar() {
  return <div className="progress" hidden={!useProgressActive()} aria-hidden="true" />
}

// 滚动超过一屏半时出现。
function BackToTop() {
  const navigationType = useNavigationType()
  const [visible, setVisible] = useState(false)
  useEffect(() => {
    const update = () => setVisible(window.scrollY > window.innerHeight * 1.5)
    // 新页面由外壳回顶，不能把上一页的滚动位置带入按钮；历史恢复仍读取当前位置。
    if (navigationType === 'POP') update()
    window.addEventListener('scroll', update, { passive: true })
    window.addEventListener('resize', update)
    return () => { window.removeEventListener('scroll', update); window.removeEventListener('resize', update) }
  }, [])
  return <button type="button" className="back-to-top" hidden={!visible} aria-label="回到顶部" onClick={() => {
    window.scrollTo({ top: 0 })
    document.getElementById('main')?.focus({ preventScroll: true })
  }}><IconArrowUp /></button>
}
