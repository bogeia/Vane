import { useSyncExternalStore } from 'react'
import data from './changelog.json'
import { createChangelogSeen, type Release } from './lib/changelog'

// 更新日志是前端内置的手写 JSON，按日期从新到旧；第一条就是最新一条。
export const releases: Release[] = data.releases
export const latestRelease = releases[0]
export const changelogSeen = createChangelogSeen(latestRelease.id, () => localStorage)

// 侧栏、手机“我的”标签和“我的”页里的更新日志红点共用这一份状态，打开 /changelog 后同时消失。
export function useChangelogUnseen() {
  useSyncExternalStore(changelogSeen.subscribe, changelogSeen.version)
  return changelogSeen.unseen()
}
