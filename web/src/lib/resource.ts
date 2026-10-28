import { useEffect, useState, useSyncExternalStore } from 'react'

// 顶部进度条：只要还有主数据在加载就显示。
let pending = 0
const listeners = new Set<() => void>()
const notify = () => { for (const listener of listeners) listener() }

export function useProgress(active: boolean) {
  useEffect(() => {
    if (!active) return
    pending++
    notify()
    return () => { pending--; notify() }
  }, [active])
}

export function useProgressActive() {
  return useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener) } }, () => pending > 0)
}

export type Resource<T> =
  | { status: 'loading' }
  | { status: 'notfound' }
  | { status: 'failed' }
  | { status: 'ok'; data: T }

// 读一个 JSON 接口。path 为 null 时不请求。组件卸载或 path 变化时取消上一次请求。
// progress 为 false 用于右栏等次要数据，不牵动顶部进度条。
export function useResource<T>(path: string | null, { progress = true } = {}): Resource<T> & { retry: () => void } {
  const [state, setState] = useState<{ path: string | null; attempt: number; result: Resource<T> }>({ path, attempt: 0, result: { status: 'loading' } })
  const [attempt, setAttempt] = useState(0)
  // path 或重试次数变了，先按加载中处理，免得这一帧显示上一次的结果。
  const result: Resource<T> = state.path === path && state.attempt === attempt ? state.result : { status: 'loading' }

  useEffect(() => {
    if (path === null) return
    const controller = new AbortController()
    const finish = (next: Resource<T>) => { if (!controller.signal.aborted) setState({ path, attempt, result: next }) }
    fetch(path, { signal: controller.signal, headers: { Accept: 'application/json' } }).then(async response => {
      // 地址里的编号或日期格式不对时接口返回 400，对读者来说同样是“不存在”。
      if (response.status === 404 || response.status === 400) return finish({ status: 'notfound' })
      if (!response.ok) return finish({ status: 'failed' })
      finish({ status: 'ok', data: await response.json() as T })
    }).catch(() => finish({ status: 'failed' }))
    return () => controller.abort()
  }, [path, attempt])

  useProgress(progress && path !== null && result.status === 'loading')
  return { ...result, retry: () => setAttempt(value => value + 1) }
}
