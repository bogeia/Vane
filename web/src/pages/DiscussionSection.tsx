import { useId } from 'react'
import type { Resource } from '../lib/resource'
import { fullTime } from '../lib/time'
import { visibleDiscussion } from './discussion'
import type { StoryDiscussion } from './discussion'

export function DiscussionSection({ discussion, status = 'ok', onRetry }: { discussion: StoryDiscussion | null | undefined; status?: Resource<unknown>['status']; onRetry?: () => void }) {
  const titleId = useId()
  const visible = visibleDiscussion(discussion)
  return <section id="discussion" className="discussion" aria-labelledby={titleId}>
    <h2 id={titleId}>HN 社区讨论</h2>
    {status === 'loading' ? <p className="discussion-note" role="status">正在加载社区讨论…</p>
      : status === 'failed' || status === 'notfound' ? <p className="discussion-note" role="alert">社区讨论暂时加载失败。{onRetry && <button type="button" className="button-secondary" onClick={onRetry}>重试</button>}</p>
      : !visible ? <p className="discussion-note">暂无可展示的社区讨论。</p>
      : <>
    <p className="discussion-note">以下要点整理自采样评论，仅代表其中的观点，不代表 HN 社区共识；评论中的说法未经独立核实。</p>
    <p className="discussion-meta"><span>采样 {visible.sample_size} 条评论</span><span>更新于 <time dateTime={visible.updated_at}>{fullTime(visible.updated_at)}</time></span></p>
    <ul>{visible.points.map((point, index) => <li key={index}>
      <p>{point.text}</p>
      <a href={point.comment_url} target="_blank" rel="noopener noreferrer">评论出处<span className="visually-hidden">（新标签页）</span></a>
    </li>)}</ul>
    <a href={visible.thread_url} target="_blank" rel="noopener noreferrer">查看 HN 原讨论<span className="visually-hidden">（新标签页）</span></a>
    </>}
  </section>
}
