// 应用内链接的统一打开入口。
//
// 两处需要它：
//   1. Monaco 编辑器里 Ctrl+点击 http(s) 链接（走 monaco.editor.registerLinkOpener）；
//   2. 渲染出来的 Markdown / HTML 里的 <a href>（文档预览、AI 回复……）——
//      这类链接在 WebView 里会**直接把当前应用页面导航走**，必须拦下来。
//
// 具体方式跟随 设置 → 通用 → 编辑器链接打开方式：
//   popup   默认：编辑器沿用 Monaco 自带的弹出窗口；<a> 没有内建行为，按「弹出窗口」处理
//   browser 系统默认浏览器（application.Browser.OpenURL）
//   window  应用内独立窗口（SiteService.OpenInApp：顶层 WebView、证书统一忽略、同名去重）
import { useSettingsStore } from '../stores/settings'
import { SiteService } from './wails'

export type LinkOpenMode = 'popup' | 'browser' | 'window'

// 读取当前设置；取不到（pinia 未就绪等）时按默认处理
export function linkOpenMode(): LinkOpenMode {
  try {
    return useSettingsStore().editorLinkOpenMode
  } catch {
    return 'popup'
  }
}

// 系统默认浏览器
export function openInSystemBrowser(url: string): void {
  SiteService.OpenInBrowser(url).catch(() => undefined)
}

// 应用内独立窗口
export function openInAppWindow(url: string, title?: string): void {
  SiteService.OpenInApp(url, title || url).catch(() => undefined)
}

/**
 * Monaco 编辑器 Ctrl+点击链接的打开方式。
 * 返回 true 表示已接管；返回 false 交回 Monaco 自带的弹出窗口。
 */
export function openEditorLink(url: string): boolean {
  const mode = linkOpenMode()
  if (mode === 'browser') {
    openInSystemBrowser(url)
    return true
  }
  if (mode === 'window') {
    openInAppWindow(url)
    return true
  }
  return false
}

let installed = false

/**
 * 拦截应用界面里 <a href="http(s)://"> 的点击：WebView 内默认导航会把整个应用
 * 页面顶掉（Markdown 预览、AI 回复里的链接都会踩到），所以接管并改走
 * 「编辑器链接打开方式」（默认档按「弹出窗口」= 应用内窗口处理）。
 * 只处理没有 target 的 http/https 链接：
 *   - 带 target（如关于页的 el-link target="_blank"）本来就在新窗口打开，不碰；
 *   - 锚点 / mailto / 自定义 scheme 也原样放行。
 */
export function installAnchorInterceptor(): void {
  if (installed) return
  installed = true
  document.addEventListener(
    'click',
    (e: MouseEvent) => {
      const target = e.target
      if (!(target instanceof Element)) return
      const anchor = target.closest('a[href]')
      if (!anchor) return
      if (anchor.hasAttribute('target')) return
      const href = anchor.getAttribute('href') || ''
      if (!/^https?:\/\//i.test(href)) return
      e.preventDefault()
      // 已接管，别再让宿主（如 Markdown 预览）自己再开一次
      e.stopPropagation()
      if (linkOpenMode() === 'browser') {
        openInSystemBrowser(href)
      } else {
        openInAppWindow(href)
      }
    },
    true,
  )
}
