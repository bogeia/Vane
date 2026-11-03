import { readFileSync } from 'node:fs'
import { validateReleases, type Release } from '../web/src/lib/changelog.ts'

const data = JSON.parse(readFileSync(new URL('../releases.json', import.meta.url), 'utf8'))
const problems = validateReleases(data)
if (problems.length) throw new Error(problems.join('\n'))
const tag = process.argv[2]
const release = (data.releases as Release[]).find(release => release.id === tag)
if (!release) throw new Error(`找不到发布记录：${tag ?? '请指定 tag'}`)
process.stdout.write(`# ${release.title}\n\n${release.date}\n\n${release.items.map(item => `- ${item}`).join('\n')}\n`)
