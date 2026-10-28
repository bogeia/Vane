import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { tierOfGroup } from '../lib/tier'
import { Columns } from '../shell/Shell'
import { TableOfContents } from '../ui/TableOfContents'
import '../ui/page.css'
import './more.css'

type SourceGroup = { title: string; sources: { name: string; url: string; rss: boolean }[] }

// 按顶层分组读出信源名和订阅或官网地址；没有信源的分组不要。
function parseOpml(text: string): SourceGroup[] {
  const parsed = new DOMParser().parseFromString(text, 'application/xml')
  if (parsed.querySelector('parsererror')) throw new Error('OPML 格式不对')
  const outlines = (parent: Element | null) => [...(parent?.children ?? [])].filter(element => element.tagName === 'outline')
  return outlines(parsed.querySelector('opml > body')).map(group => ({
    title: group.getAttribute('title') || group.getAttribute('text') || '',
    sources: outlines(group)
      .map(source => ({
        name: source.getAttribute('title') || source.getAttribute('text') || '',
        url: source.getAttribute('xmlUrl') || source.getAttribute('htmlUrl') || '',
        rss: !!source.getAttribute('xmlUrl'),
      }))
      .filter(source => source.name && /^https?:\/\//i.test(source.url)),
  })).filter(group => group.sources.length > 0)
}

// 信源列表读 /sources.opml。只有这一段会显示加载失败和重试，页面其他部分照常显示。
function useSourceGroups() {
  const [attempt, setAttempt] = useState(0)
  const [state, setState] = useState<{ attempt: number; groups: SourceGroup[] | null; failed: boolean }>({ attempt: -1, groups: null, failed: false })
  useEffect(() => {
    const controller = new AbortController()
    fetch('/sources.opml', { signal: controller.signal })
      .then(response => { if (!response.ok) throw new Error(`OPML ${response.status}`); return response.text() })
      .then(text => setState({ attempt, groups: parseOpml(text), failed: false }))
      .catch(() => { if (!controller.signal.aborted) setState({ attempt, groups: null, failed: true }) })
    return () => controller.abort()
  }, [attempt])
  const current = state.attempt === attempt ? state : { attempt, groups: null, failed: false }
  return { groups: current.groups, failed: current.failed, retry: () => setAttempt(value => value + 1) }
}

const CATEGORY_FEEDS = [['模型', 'model'], ['产品', 'product'], ['行业', 'industry'], ['论文', 'paper'], ['教程', 'tutorial'], ['观点', 'opinion']]

// 关于 /about。
export function AboutPage() {
  const { groups, failed, retry } = useSourceGroups()
  const total = groups?.reduce((sum, group) => sum + group.sources.length, 0)
  const rail = <TableOfContents>
    <li><a href="#about-what">Vane 是什么</a></li>
    <li><a href="#about-how">资料怎么挑出来</a></li>
    <li><a href="#about-sources">信源</a></li>
    <li><a href="#about-feeds">订阅</a></li>
    <li><a href="#about-data">你的数据</a></li>
    <li><a href="#about-thanks">致谢</a></li>
  </TableOfContents>
  return <Columns rail={rail}><article className="prose-page about">
    <header className="page-head"><div><h1>关于 Vane</h1><p>它做什么、资料怎么挑出来、有哪些信源。</p></div></header>

    <section aria-labelledby="about-what">
      <h2 id="about-what">Vane 是什么</h2>
      <p>Vane 是风向标。它每天从{total ? ` ${total} 个` : ''} AI 信源收集资料，用大模型判断哪些值得看，写成中文标题、摘要和推荐理由，把不同信源说同一件事的资料归成事件，并在每天 08:00（北京时间）出一期日报。</p>
    </section>

    <section aria-labelledby="about-how">
      <h2 id="about-how">资料怎么挑出来</h2>
      <ol className="about-steps">
        <li><b>收集</b>：定时抓取各信源的 RSS、网页列表或模型发布。运营者给个别信源设了屏蔽词，命中的内容不收。</li>
        <li><b>预筛</b>：模型判断资料和 AI 有没有关系，无关的拦下。</li>
        <li><b>评分</b>：模型独立给每条资料打两次分，0 到 100。</li>
        <li><b>写作</b>：模型写中文标题、摘要和推荐理由，给出分类和标签，并判断是不是综合稿。</li>
        <li><b>精选</b>：两次评分之和达到所属信源分级门槛的两倍，而且不是综合稿，就进精选。每个信源分级有自己的门槛。</li>
        <li><b>事件和热度</b>：不同信源说同一件事的资料归成一个事件。至少两家信源参与的事件按热度进热点榜，热度每 24 小时减半。</li>
        <li><b>日报</b>：每天 08:00 把前一日 08:00 到当日 08:00 处理完的精选编成一期，有头条、看点、分类正文和快讯。</li>
      </ol>
    </section>

    <section aria-labelledby="about-sources">
      <h2 id="about-sources">信源</h2>
      <p>按信源分级分组，和 <a href="/sources.opml">/sources.opml</a> 一致。</p>
      {!groups && !failed && <p className="about-note" role="status">正在读取信源列表…</p>}
      {failed && <div className="about-failed" role="alert">
        <p>信源列表暂时读不出来，可能是网络断了，或者服务正在重启。</p>
        <button type="button" className="button-secondary" onClick={retry}>重试</button>
      </div>}
      {groups?.map(group => {
        const tier = tierOfGroup(group.title)
        return <section className="source-group" key={group.title} aria-label={group.title}>
          <h3>{group.title}<span>{group.sources.length} 个</span></h3>
          {tier && <p className="about-note">资料详情上标为「{tier.short}」</p>}
          <ul>{group.sources.map(source => <li key={source.url}>
            <span>{source.name}</span>
            <a href={source.url} target="_blank" rel="noopener noreferrer" aria-label={`${source.name} 的${source.rss ? '订阅地址' : '官网'}（新标签页）`}>{source.rss ? 'RSS' : '官网'} ↗</a>
          </li>)}</ul>
        </section>
      })}
    </section>

    <section aria-labelledby="about-feeds">
      <h2 id="about-feeds">订阅</h2>
      <ul className="about-feeds">
        <li>精选：<a href="/feed.xml">/feed.xml</a></li>
        <li>全部动态：<a href="/feed/all.xml">/feed/all.xml</a></li>
        <li>日报：<a href="/feed/daily.xml">/feed/daily.xml</a></li>
        <li>分类精选：{CATEGORY_FEEDS.map(([name, slug], index) => <span key={slug}>{index > 0 && '、'}<a href={`/feed/category/${slug}.xml`}>{name}</a></span>)}</li>
        <li>信源列表：<a href="/sources.opml">/sources.opml</a></li>
      </ul>
    </section>

    <section aria-labelledby="about-data">
      <h2 id="about-data">你的数据</h2>
      <p>收藏和已读只存在你自己的浏览器里，不上传，也不跨设备同步。打开页面只读取已经处理好的数据，不会触发模型调用。</p>
    </section>

    <section aria-labelledby="about-thanks">
      <h2 id="about-thanks">致谢</h2>
      <p>Vane 借鉴了 <a href="https://github.com/KKKKhazix/AIHOT" target="_blank" rel="noopener noreferrer">AIHOT</a>（MIT 许可）的选题思路、预筛和评分提示词、分类说明以及主题和标签词表，借鉴的内容以仓库里的 NOTICE 为准。也参考了 ai-news-radar、Horizon、TrendRadar、Folo、Miniflux 的做法。</p>
      <p className="about-note">更新记录见<Link to="/changelog">更新日志</Link>。</p>
    </section>
  </article></Columns>
}
