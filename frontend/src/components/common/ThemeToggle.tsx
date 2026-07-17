import { useEffect, useState } from 'react'
import AppIcon from './AppIcon'

export type AppTheme = 'light' | 'dark'

const THEME_STORAGE_KEY = 'theme'

// 首次进入时优先复用用户选择，否则遵循操作系统主题。
export const resolveInitialTheme = (savedTheme: string | null, prefersDark: boolean): AppTheme => {
  if (savedTheme === 'light' || savedTheme === 'dark') {
    return savedTheme
  }
  return prefersDark ? 'dark' : 'light'
}

export const getNextTheme = (theme: AppTheme): AppTheme =>
  theme === 'light' ? 'dark' : 'light'

const ThemeToggle = () => {
  const [theme, setTheme] = useState<AppTheme>(() => {
    if (typeof window === 'undefined') return 'light'
    return resolveInitialTheme(
      window.localStorage.getItem(THEME_STORAGE_KEY),
      window.matchMedia('(prefers-color-scheme: dark)').matches,
    )
  })

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    window.localStorage.setItem(THEME_STORAGE_KEY, theme)
  }, [theme])

  const nextTheme = getNextTheme(theme)

  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={() => setTheme(nextTheme)}
      aria-label={`切换到${nextTheme === 'dark' ? '深色' : '浅色'}模式`}
      title={`切换到${nextTheme === 'dark' ? '深色' : '浅色'}模式`}
    >
      <AppIcon name={theme === 'light' ? 'moon' : 'sun'} size={20} />
    </button>
  )
}

export default ThemeToggle
