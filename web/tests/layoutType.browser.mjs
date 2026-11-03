import assert from 'node:assert/strict'
import { mockDesignData } from './redesign.browser.mjs'

// Run with a Playwright page against either Vite or the production preview.
export async function testLayoutType(page, baseURL) {
  await mockDesignData(page)
  let previousRailWidth = 0
  for (const width of [1181, 1440, 1920, 2560]) {
    await page.setViewportSize({ width, height: 1080 })
    await page.goto(new URL('/', baseURL).href)
    await page.locator('.card-title').first().waitFor()
    const layout = await page.evaluate(() => {
      const rect = selector => document.querySelector(selector).getBoundingClientRect()
      return {
        gutter: rect('.shell-main').right - rect('.shell-content').right,
        extraLeft: rect('.shell-content').left - rect('.shell-main').left - 40,
        extraRight: rect('.shell-main').right - rect('.shell-content').right - 32,
        content: rect('.shell-content').width,
        rail: rect('.columns-rail').width,
        main: rect('.columns-main').width,
        overflow: document.documentElement.scrollWidth > innerWidth,
      }
    })
    assert.ok(layout.gutter >= 32, 'outer gutter stays available')
    assert.ok(Math.abs(layout.extraLeft - layout.extraRight) < 1, 'wide layouts center remaining space')
    assert.ok(layout.content <= 1360, 'reading layout does not stretch indefinitely')
    assert.ok(layout.rail >= 260 && layout.rail <= 360 && layout.rail < layout.main / 2)
    assert.ok(layout.rail >= previousRailWidth, 'rail grows until its readable width limit')
    assert.equal(layout.overflow, false)
    previousRailWidth = layout.rail
    const typography = await page.evaluate(() => {
      const style = selector => getComputedStyle(document.querySelector(selector))
      return {
        body: style('body').fontFamily,
        title: style('.card-title').fontFamily,
        date: style('.day-head').fontFamily,
        score: style('.score').fontFamily,
        weight: Number(style('.card-title').fontWeight),
      }
    })
    assert.equal(typography.title, typography.body)
    assert.equal(typography.date, typography.body)
    assert.equal(typography.score, typography.body)
    assert.ok(typography.weight <= 600, 'headlines avoid heavy Latin text')
  }
}
