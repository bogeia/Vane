import { useEffect, useRef, useState } from 'react'
import { Columns } from '../shell/Shell'
import { TableOfContents } from '../ui/TableOfContents'
import '../ui/page.css'
import './more.css'
import './agent.css'

const TOOLS = [
  ['vane_get_latest', '查过去 24 小时或最近 7 天的精选、全部动态，可按主题筛选。'],
  ['vane_search', '按关键词搜索，先查精选，精选没有再查全部动态。'],
  ['vane_get_hot_topics', '按名次查看当前热点榜。'],
  ['vane_get_story', '了解某个热点的最新进展和报道时间线。'],
  ['vane_get_daily', '读最新一期或指定日期的日报。'],
]
const SUBSCRIPTIONS = [
  ['精选 RSS', '/feed.xml'], ['全部动态 RSS', '/feed/all.xml'], ['日报 RSS', '/feed/daily.xml'],
  ['模型精选 RSS', '/feed/category/model.xml'], ['产品精选 RSS', '/feed/category/product.xml'],
  ['行业精选 RSS', '/feed/category/industry.xml'], ['论文精选 RSS', '/feed/category/paper.xml'],
  ['教程精选 RSS', '/feed/category/tutorial.xml'], ['观点精选 RSS', '/feed/category/opinion.xml'],
  ['信源列表 OPML', '/sources.opml'],
]

export function AgentPage() {
  const origin = window.location.origin
  const configuration = JSON.stringify({ mcpServers: { vane: { type: 'http', url: `${origin}/api/mcp` } } }, null, 2)
  const rail = <TableOfContents>
    <li><a href="#agent-mcp">支持 MCP 的客户端</a></li>
    <li><a href="#agent-web">只能读网页的助手</a></li>
    <li><a href="#agent-subscriptions">订阅</a></li>
    <li><a href="#agent-api">程序接口</a></li>
  </TableOfContents>
  return <Columns rail={rail}><article className="prose-page agent-page">
    <header className="page-head"><div><h1>接入 Agent</h1><p>让你的 AI 助手直接读 Vane 的精选、热点和日报</p></div></header>
    <section aria-labelledby="agent-mcp">
      <h2 id="agent-mcp">支持 MCP 的客户端</h2>
      <p>不需要 API Key。复制命令接入 Claude Code，或把通用 JSON 配置填入支持 Streamable HTTP 的客户端。</p>
      <CopyBlock label="Claude Code 命令" text={`claude mcp add --transport http vane ${origin}/api/mcp`} />
      <CopyBlock label="通用 JSON 配置" text={configuration} />
      <dl className="agent-tools">{TOOLS.map(([name, description]) => <div key={name}>
        <dt><code>{name}</code></dt><dd>{description}</dd>
      </div>)}</dl>
    </section>
    <section aria-labelledby="agent-web">
      <h2 id="agent-web">只能读网页的助手</h2>
      <p>把下面这句话发给助手，让它先读使用说明，再按你的问题查询。</p>
      <CopyBlock label="给助手的那句话" text={`请先读 ${origin}/api/agent ，按里面的说明回答我的问题。`} />
    </section>
    <section aria-labelledby="agent-subscriptions">
      <h2 id="agent-subscriptions">订阅</h2>
      <p>在自己的阅读器里订阅 RSS，或导入信源列表。</p>
      {SUBSCRIPTIONS.map(([label, path]) => <CopyBlock key={path} label={label} text={origin + path} href={origin + path} />)}
    </section>
    <section aria-labelledby="agent-api">
      <h2 id="agent-api">程序接口</h2>
      <p>写程序同步或推送，请使用 RSS 或现有 JSON 接口。JSON 字段可能调整；llms.txt 为大模型提供站点与接入指引。</p>
      <CopyBlock label="OpenAPI" text={`${origin}/openapi.json`} href={`${origin}/openapi.json`} />
      <CopyBlock label="llms.txt" text={`${origin}/llms.txt`} href={`${origin}/llms.txt`} />
    </section>
  </article></Columns>
}

function CopyBlock({ label, text, href }: { label: string; text: string; href?: string }) {
  const content = useRef<HTMLElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const [status, setStatus] = useState('复制')
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const selectText = () => {
    if (!content.current) return
    content.current.focus()
    const range = document.createRange()
    range.selectNodeContents(content.current)
    const selection = window.getSelection()
    selection?.removeAllRanges()
    selection?.addRange(range)
    setStatus('请手动复制')
  }
  const copy = async () => {
    window.clearTimeout(timer.current)
    if (!navigator.clipboard?.writeText) return selectText()
    try {
      await navigator.clipboard.writeText(text)
      if (!content.current) return
      window.clearTimeout(timer.current)
      setStatus('已复制')
      timer.current = window.setTimeout(() => setStatus('复制'), 2000)
    } catch {
      selectText()
    }
  }
  return <div className="agent-copy">
    <div className="agent-copy-head"><h3>{label}</h3>
      <button type="button" className="button-secondary" onClick={copy} aria-label={`${status}：${label}`} aria-live="polite">{status}</button>
    </div>
    <pre><code ref={content} tabIndex={0}>{href ? <a href={href}>{text}</a> : text}</code></pre>
  </div>
}
