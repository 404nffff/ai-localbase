import { describe, expect, it } from 'vitest'
import { getNextTheme, resolveInitialTheme } from './ThemeToggle'

describe('ThemeToggle preference', () => {
  it('优先使用已保存的主题', () => {
    expect(resolveInitialTheme('light', true)).toBe('light')
    expect(resolveInitialTheme('dark', false)).toBe('dark')
  })

  it('没有有效保存值时遵循系统主题', () => {
    expect(resolveInitialTheme(null, true)).toBe('dark')
    expect(resolveInitialTheme('unknown', false)).toBe('light')
  })

  it('在亮暗主题之间切换', () => {
    expect(getNextTheme('light')).toBe('dark')
    expect(getNextTheme('dark')).toBe('light')
  })
})
