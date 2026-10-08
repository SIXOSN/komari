import { expect, test } from '@playwright/test'
import { installKomariFixture } from './fixtures/komari'

test.use({
  viewport: { width: 390, height: 740 },
  isMobile: true,
  hasTouch: true,
  userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36 EdgA/138.0.0.0',
})

test('mobile visitor entry stays horizontally centered throughout its animation', async ({ page }) => {
  await installKomariFixture(page, { hideEarth: true })
  await page.addInitScript(() => {
    const frames: Array<{ centerOffset: number, top: number }> = []
    Object.assign(window, { visitorEntryFrames: frames })
    function sample() {
      const bar = document.querySelector('.slide-up-enter-active')
      if (bar) {
        const rect = bar.getBoundingClientRect()
        frames.push({ centerOffset: rect.left + rect.width / 2 - window.innerWidth / 2, top: rect.top })
      }
      requestAnimationFrame(sample)
    }
    requestAnimationFrame(sample)
  })
  await page.goto('/')
  const bar = page.locator('.fixed.rounded-full').filter({ hasText: '2001:db8::25' })
  await expect(bar).toBeVisible()
  await expect(page.locator('.slide-up-enter-active')).toHaveCount(0)
  const readFrames = () => page.evaluate(() => (window as unknown as {
    visitorEntryFrames: Array<{ centerOffset: number, top: number }>
  }).visitorEntryFrames)
  const initialFrames = await readFrames()
  expect(initialFrames.length).toBeGreaterThan(3)
  expect(Math.max(...initialFrames.map(frame => Math.abs(frame.centerOffset)))).toBeLessThanOrEqual(1)

  // Also check the entry after scrolling hides and then reveals the bar.
  await page.evaluate(() => window.scrollTo({ top: 400, behavior: 'instant' }))
  await expect.poll(async () => (await readFrames()).length).toBeGreaterThan(initialFrames.length)
  await expect(bar).toBeVisible()
  await expect(page.locator('.slide-up-enter-active')).toHaveCount(0)
  const frames = await readFrames()
  expect(frames.length).toBeGreaterThan(3)
  expect(Math.max(...frames.map(frame => Math.abs(frame.centerOffset)))).toBeLessThanOrEqual(1)
  expect(Math.max(...frames.map(frame => frame.top)) - Math.min(...frames.map(frame => frame.top))).toBeGreaterThan(5)
})

for (const dark of [false, true]) {
  test(`mobile ${dark ? 'dark' : 'light'} background covers scrolling and viewport changes`, async ({ page }, testInfo) => {
    await installKomariFixture(page, { dark, hideEarth: true })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Komari Visual Lab' })).toBeVisible()
    await expect(page.locator('.default-background')).toBeVisible()
    for (const viewport of [{ width: 390, height: 740 }, { width: 390, height: 844 }, { width: 844, height: 390 }]) {
      await page.setViewportSize(viewport)
      await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
      await expect.poll(() => page.locator('.background-container').evaluate((element) => {
        const bounds = element.getBoundingClientRect()
        const viewport = window.visualViewport
        const left = viewport?.offsetLeft ?? 0
        const top = viewport?.offsetTop ?? 0
        return bounds.left <= left && bounds.top <= top
          && bounds.right >= left + (viewport?.width ?? window.innerWidth) - 1
          && bounds.bottom >= top + (viewport?.height ?? window.innerHeight) - 1
      })).toBe(true)
      expect(await page.locator('html').evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
    }
    await page.setViewportSize({ width: 390, height: 844 })
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    await expect(page.locator('.slide-up-enter-active, .slide-up-leave-active')).toHaveCount(0)
    const screenshot = testInfo.outputPath('mobile-page-bottom.png')
    await page.screenshot({ path: screenshot })
    await testInfo.attach('mobile-page-bottom', { path: screenshot, contentType: 'image/png' })
  })
}
