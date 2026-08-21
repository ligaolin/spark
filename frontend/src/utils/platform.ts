// 平台检测工具：用于在移动端屏蔽桌面专属功能（如 GitHub 更新检查）。

// 判断当前是否运行在 Android 应用里（Wails Android 通过 JavascriptInterface
// 暴露 window.wails.platform() === "android"）。
export function isAndroidApp(): boolean {
  try {
    const w = (window as any)?.wails
    if (w && typeof w.platform === 'function') {
      return w.platform() === 'android'
    }
  } catch {
    /* ignore */
  }
  // 兜底：Android WebView 的 UA 含 Android
  return /android/i.test(navigator.userAgent || '')
}
