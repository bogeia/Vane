import test, { type TestContext } from 'node:test'
import assert from 'node:assert/strict'
import { createSharedRead } from '../src/lib/sharedRead.ts'

function network(t: TestContext) {
  const requests: { signal: AbortSignal; resolve: (response: Response) => void }[] = []
  t.mock.method(globalThis, 'fetch', (_path: string, options: RequestInit) => new Promise<Response>(resolve => {
    requests.push({ signal: options.signal as AbortSignal, resolve })
  }))
  const settle = async (index: number, data: unknown, status = 200) => {
    requests[index].resolve(new Response(JSON.stringify(data), { status }))
    await new Promise(resolve => setImmediate(resolve))
  }
  return { requests, settle }
}

test('多个读者共享请求与结果，卸载一个读者不取消其他读者', async t => {
  const { requests, settle } = network(t)
  const read = createSharedRead<{ value: number }>('/api/topics')
  let notifications = 0
  const leaveFirst = read.subscribe(() => {})
  const leaveSecond = read.subscribe(() => { notifications++ })
  assert.equal(requests.length, 1)
  leaveFirst()
  await Promise.resolve()
  assert.equal(requests[0].signal.aborted, false)
  await settle(0, { value: 1 })
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { value: 1 } })
  assert.equal(notifications, 1)
  leaveSecond()
  await Promise.resolve()
  assert.deepEqual(read.getSnapshot(), { status: 'loading' })
})

test('共享失败可以重试，重复重试只保留一份活跃请求', async t => {
  const { requests, settle } = network(t)
  const read = createSharedRead('/api/reports')
  const leave = read.subscribe(() => {})
  await settle(0, {}, 500)
  assert.equal(read.getSnapshot().status, 'failed')
  read.refresh()
  read.refresh()
  assert.equal(read.getSnapshot().status, 'loading')
  assert.equal(requests.length, 2)
  await settle(1, { items: [] })
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { items: [] } })
  leave()
  await Promise.resolve()
})

test('最后读者卸载取消请求，重新进入更新，旧结果不能覆盖新结果', async t => {
  const { requests, settle } = network(t)
  const read = createSharedRead('/api/topics')
  const leave = read.subscribe(() => {})
  leave()
  await Promise.resolve()
  assert.equal(requests[0].signal.aborted, true)
  const leaveAgain = read.subscribe(() => {})
  assert.equal(requests.length, 2)
  await settle(1, { fresh: true })
  await settle(0, { old: true })
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { fresh: true } })
  leaveAgain()
  await Promise.resolve()
  const leaveThird = read.subscribe(() => {})
  assert.equal(read.getSnapshot().status, 'loading')
  assert.equal(requests.length, 3)
  leaveThird()
  await Promise.resolve()
})

test('同次路由交接保留就绪数据，再校验更新且失败保留已得数据', async t => {
  const { requests, settle } = network(t)
  const read = createSharedRead('/api/topics')
  const leave = read.subscribe(() => {})
  await settle(0, { version: 1 })
  leave()
  const leaveNext = read.subscribe(() => {})
  await Promise.resolve()
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { version: 1 } })
  assert.equal(requests.length, 1)
  read.refresh()
  assert.equal(read.getSnapshot().status, 'ok')
  await settle(1, {}, 503)
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { version: 1 } })
  read.refresh()
  await settle(2, { version: 2 })
  assert.deepEqual(read.getSnapshot(), { status: 'ok', data: { version: 2 } })
  leaveNext()
  await Promise.resolve()
  read.refresh()
  assert.equal(requests.length, 3, '没有读者时不发请求')
})
