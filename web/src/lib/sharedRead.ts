import type { Resource } from './resource.ts'

// 只在有读者时保留结果；微任务宽限让同次路由切换和 StrictMode 交接读者。
export function createSharedRead<T>(path: string) {
  let result: Resource<T> = { status: 'loading' }
  let controller: AbortController | undefined
  const listeners = new Set<() => void>()
  const publish = (next: Resource<T>) => {
    result = next
    for (const listener of listeners) listener()
  }
  const load = () => {
    if (controller || listeners.size === 0) return
    const request = new AbortController()
    controller = request
    const finish = (next: Resource<T>) => {
      if (controller !== request || request.signal.aborted) return
      controller = undefined
      // 已就绪数据再校验失败时保留，首次加载失败仍交给主页面展示与重试。
      if (next.status === 'ok' || result.status !== 'ok') publish(next)
    }
    if (result.status !== 'ok') publish({ status: 'loading' })
    fetch(path, { signal: request.signal, headers: { Accept: 'application/json' } }).then(async response => {
      if (response.status === 404 || response.status === 400) return finish({ status: 'notfound' })
      if (!response.ok) return finish({ status: 'failed' })
      finish({ status: 'ok', data: await response.json() as T })
    }).catch(() => finish({ status: 'failed' }))
  }
  return {
    getSnapshot: () => result,
    subscribe(listener: () => void) {
      listeners.add(listener)
      if (result.status !== 'ok') load()
      return () => {
        listeners.delete(listener)
        queueMicrotask(() => {
          if (listeners.size > 0) return
          controller?.abort()
          controller = undefined
          result = { status: 'loading' }
        })
      }
    },
    refresh: load,
  }
}
