// 资料列表、详情、日报、事件接口里的一条资料。
export type Item = {
  id: string
  title: string
  original_title: string
  summary: string
  reason: string
  url: string
  image_url: string
  source: { id: string; name: string; tier: string; icon_url: string }
  published_at: string
  score: number
  selected: boolean
  category: string
  tags: string[]
  story_id: string
  story_others: number
  story_item_ids: string[]
}

export type View = 'selected' | 'all'
