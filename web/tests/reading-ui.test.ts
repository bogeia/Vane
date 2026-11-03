import test, { after } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createServer } from 'vite'

// 通过 Vite 的同一 JSX 转换读取组件；不监听端口，也不共享开发服务缓存。
const cacheDir = await mkdtemp(join(tmpdir(), 'vane-reading-ui-'))
const server = await createServer({ configFile: false, root: fileURLToPath(new URL('../', import.meta.url)), plugins: [react()], cacheDir, server: { middlewareMode: true }, appType: 'custom' })
after(async () => { await server.close(); await rm(cacheDir, { recursive: true, force: true }) })
const { CollectedBody } = await server.ssrLoadModule('/src/pages/CollectedBody.tsx')
const { DiscussionSection } = await server.ssrLoadModule('/src/pages/DiscussionSection.tsx')

function body(text: string) { return renderToStaticMarkup(createElement(CollectedBody, { body: text })) }
function discussion(status: string, data: unknown = null) { return renderToStaticMarkup(createElement(DiscussionSection, { discussion: data, status, onRetry() {} })) }

test('采集正文默认折叠，HTML作为文字输出，保留段落和长内容', () => {
  const text = '<script>alert(1)</script>\n\n第二段\n' + 'a'.repeat(2000)
  const html = body(text)
  assert.match(html, /<details id="body" class="detail-block detail-body">/)
  assert.doesNotMatch(html, /<details[^>]*\bopen\b|<script>/)
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;<\/p><p class="detail-body-text">第二段\n/)
  assert.ok(html.includes('a'.repeat(2000)))
  assert.match(html, /采集内容可能不完整/)
  assert.match(body(' \n '), /暂无可展示的采集正文/)
})

test('采集正文以空行分段，合并多余空段并保留段内换行和列表', () => {
  const html = body(' \r\n\r\n第一段\r\n第二行\r\n \r\n\r\n- 列表一\r\n- 列表二\r\n\r\n  ')
  assert.equal((html.match(/class="detail-body-text"/g) ?? []).length, 2)
  assert.match(html, /<p class="detail-body-text">第一段\n第二行<\/p>/)
  assert.match(html, /<p class="detail-body-text">- 列表一\n- 列表二<\/p>/)
  assert.doesNotMatch(html, /\r/)
})

test('正文与讨论提供稳定锚点，仅正文直达请求默认展开', () => {
  const html = renderToStaticMarkup(createElement(CollectedBody, { body: '完整正文', openFromLink: true }))
  assert.match(html, /<details id="body"[^>]* open=""/)
  assert.match(html, /完整正文/)
  assert.match(discussion('ok'), /<section id="discussion"/)
})

test('社区讨论空、加载、失败和有出处观点分别展示', () => {
  assert.match(discussion('ok'), /暂无可展示的社区讨论/)
  assert.doesNotMatch(discussion('ok'), /未采集|无匹配|未关联|重试/)
  assert.match(discussion('loading'), /role="status".*正在加载社区讨论/)
  for (const status of ['failed', 'notfound']) {
    assert.match(discussion(status), /role="alert".*社区讨论暂时加载失败/)
    assert.match(discussion(status), />重试<\/button>/)
    assert.doesNotMatch(discussion(status), /暂无可展示/)
  }
  const html = discussion('ok', { thread_url: 'https://news.ycombinator.com/item?id=123', sample_size: 3, updated_at: '2026-11-03T00:00:00Z', points: [{ text: '<b>观点</b>', comment_url: 'https://news.ycombinator.com/item?id=456' }] })
  assert.match(html, /&lt;b&gt;观点&lt;\/b&gt;/)
  assert.match(html, /href="https:\/\/news.ycombinator.com\/item\?id=456"/)
  assert.match(html, /不代表 HN 社区共识/)
})
