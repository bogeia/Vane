import type { View } from '../items/types'

export function defaultTopicView(topic: { selected_count: number; all_count: number }): View {
  return topic.selected_count === 0 && topic.all_count > 0 ? 'all' : 'selected'
}
