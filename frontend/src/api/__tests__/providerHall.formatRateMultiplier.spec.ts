import { describe, it, expect } from 'vitest'
import { formatRateMultiplier } from '../providerHall'

describe('formatRateMultiplier', () => {
  it('不补零：有几位显示几位', () => {
    expect(formatRateMultiplier(0.1)).toBe('0.1')
    expect(formatRateMultiplier(0.06)).toBe('0.06')
    expect(formatRateMultiplier(1)).toBe('1')
    expect(formatRateMultiplier(1.5)).toBe('1.5')
  })

  it('不截断：小数位多的完整显示', () => {
    expect(formatRateMultiplier(0.045)).toBe('0.045')
    expect(formatRateMultiplier(0.0425)).toBe('0.0425')
    expect(formatRateMultiplier(0.00125)).toBe('0.00125')
  })

  it('抹掉浮点噪声', () => {
    expect(formatRateMultiplier(0.1 * 3)).toBe('0.3')
  })

  it('零和非法值', () => {
    expect(formatRateMultiplier(0)).toBe('0')
    expect(formatRateMultiplier(Number.NaN)).toBe('—')
  })
})
