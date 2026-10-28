import { useState } from 'react'
import type { FavoriteItem } from '../readingState'
import { useReadingState } from '../useReadingState'

// 收藏或取消收藏一条资料；收藏满 1000 条时给出提示。
export function useFavorite(item: FavoriteItem) {
  const reading = useReadingState()
  const [message, setMessage] = useState('')
  const saved = reading.isFavorite(item.id)
  const toggle = () => {
    setMessage('')
    if (saved) reading.removeFavorite(item.id)
    else if (reading.addFavorite(item) === 'full') setMessage('收藏已满，请先清理')
  }
  return { saved, toggle, message }
}
