import { useSyncExternalStore } from 'react'

export function useMediaQuery(query: string) {
  return useSyncExternalStore(listener => {
    const list = matchMedia(query)
    list.addEventListener('change', listener)
    return () => list.removeEventListener('change', listener)
  }, () => matchMedia(query).matches)
}
