// 明暗主题工具：负责在 <html> 上切换 Element Plus 暗色所需的 .dark 类，
// 并提供 localStorage 快速路径（应用启动时同步读取，避免首屏闪主题）。
// 持久化主存储是数据库 settings 表（app.theme），localStorage 只作启动缓存。

export type Theme = 'dark' | 'light'

export const THEME_STORAGE_KEY = 'spark.theme'

export const DEFAULT_THEME: Theme = 'dark'

/** 将主题类应用到 <html>，返回是否暗色 */
export function applyTheme(theme: Theme): boolean {
  const dark = theme === 'dark'
  document.documentElement.classList.toggle('dark', dark)
  return dark
}

/** 读取 localStorage 中缓存的主题（无缓存时用默认暗色，保持现有行为） */
export function getCachedTheme(): Theme {
  try {
    return localStorage.getItem(THEME_STORAGE_KEY) === 'light' ? 'light' : DEFAULT_THEME
  } catch {
    return DEFAULT_THEME
  }
}

/** 缓存主题到 localStorage（供下次启动快速应用） */
export function cacheTheme(theme: Theme) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // localStorage 不可用时忽略，主题仍会通过设置存储生效
  }
}
