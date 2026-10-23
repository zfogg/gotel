import { chromium } from 'playwright'

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
  await page.getByRole('button', { name: 'Count words' }).click()
  await page.locator('#result[data-state="done"]').waitFor({ timeout: 30000 })
  const result = await page.locator('#result').textContent()
  if (result !== '4 words') throw new Error(`unexpected result: ${result}`)
  console.log(`Browser → Go API → C worker: ${result}`)
} finally {
  await browser.close()
}
