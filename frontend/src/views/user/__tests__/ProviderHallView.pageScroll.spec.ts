import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(resolve(here, '../ProviderHallView.vue'), 'utf8')
const cssSource = readFileSync(resolve(here, '../../../styles/table-page-scroll.css'), 'utf8')

/**
 * 「渠道状态」hybrid 页要求整页滚动（分组一多，滚轮不能困在表体里）。
 * 这套接线很容易在 rebase 或重构里被顺手抹掉，所以在源码层面钉住。
 */
describe('ProviderHallView 整页滚动接线', () => {
  it('给 TablePageLayout 挂上 table-page-scroll 并引入对应样式', () => {
    expect(viewSource).toMatch(/<TablePageLayout[^>]*class="table-page-scroll"/)
    expect(viewSource).toContain("import '@/styles/table-page-scroll.css'")
  })

  it('关掉 DataTable 虚拟滚动：虚拟器依赖 .table-wrapper 当滚动容器', () => {
    expect(viewSource).toMatch(/:virtualize-threshold="Infinity"/)
  })
})

describe('table-page-scroll.css', () => {
  it('只在 ≥1024px 生效，不碰 768–1024 那段表格的横向滚动', () => {
    expect(cssSource).toMatch(/@media \(min-width: 1024px\)/)
    const guarded = cssSource.slice(cssSource.indexOf('@media (min-width: 1024px)'))
    expect(guarded).toContain('.table-page-scroll .table-wrapper')
  })

  it('拆掉一屏高的锁、撤掉表体 overflow、表头改吸窗口顶部', () => {
    expect(cssSource).toMatch(/\.table-page-scroll\.table-page-layout\s*\{\s*height: auto !important;/)
    expect(cssSource).toMatch(/\.table-page-scroll \.table-wrapper\s*\{\s*overflow: visible !important;/)
    // 4rem = AppHeader 的 h-16，表头要停在它下面而不是被它盖住
    expect(cssSource).toMatch(/\.sticky-header-cell\s*\{\s*top: 4rem !important;/)
  })
})
