export type StoryDiscussion = {
  thread_url: string
  sample_size: number
  updated_at: string
  points: { text: string; comment_url: string }[]
}

function isHNLink(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && url.hostname === 'news.ycombinator.com' && !url.username && !url.password && !url.port && url.pathname === '/item' && /^[1-9]\d*$/.test(url.searchParams.get('id') ?? '')
  } catch { return false }
}

// 只展示带有效出处的要点；缓存时效由事件接口决定。
export function visibleDiscussion(discussion: StoryDiscussion | null | undefined): StoryDiscussion | null {
  if (!discussion || !isHNLink(discussion.thread_url) || !Number.isInteger(discussion.sample_size) || discussion.sample_size < 2 || !Number.isFinite(Date.parse(discussion.updated_at))) return null
  const points = discussion.points.filter(point => point.text.trim() && isHNLink(point.comment_url)).slice(0, 3)
  return points.length ? { ...discussion, points } : null
}
