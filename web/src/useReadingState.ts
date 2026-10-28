import { useSyncExternalStore } from 'react'
import { createReadingState } from './readingState'

export const readingState = createReadingState()

export function useReadingState() {
  useSyncExternalStore(readingState.subscribe, readingState.version)
  return readingState
}
