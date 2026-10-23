import { chromium } from 'playwright'
import { writeFileSync } from 'node:fs'

const browser = await chromium.launch({ headless: true })
try {
  const page = await browser.newPage()
  page.on('console', message => console.log(`browser console: ${message.type()} ${message.text()}`))
  page.on('pageerror', error => console.error(`browser error: ${error.message}`))
  page.on('requestfailed', request => console.error(`browser request failed: ${request.url()} ${request.failure()?.errorText}`))
  page.on('response', response => {
    if (response.status() >= 400) console.error(`browser HTTP ${response.status()}: ${response.url()}`)
  })
  await page.goto('http://browser/', { waitUntil: 'networkidle' })
  const request = page.waitForResponse(response => response.url().endsWith('/api/count') && response.request().method() === 'POST')
  await page.getByRole('button', { name: 'Count words' }).click()
  const payload = await (await request).json()
  if (!/^[0-9a-f]{32}$/.test(payload.trace_id)) throw new Error(`invalid trace ID: ${payload.trace_id}`)
  await page.locator('#result[data-state="done"]').waitFor({ timeout: 30000 })
  const result = await page.locator('#result').textContent()
  if (result !== '4 words') throw new Error(`unexpected result: ${result}`)
  writeFileSync('/shared/trace-id', payload.trace_id)
  console.log(`Browser → Go API → C worker: ${result}`)
} finally {
  await browser.close()
}
