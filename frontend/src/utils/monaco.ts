// Monaco 编辑器懒加载与 Worker 环境配置。
//
// 桌面应用离线运行,语言服务 Worker 必须随应用一起打包(不能走 CDN),
// 这里用 Vite 的 ?worker 导入把各 Worker 打进构建产物,并让 Monaco
// 通过 MonacoEnvironment.getWorker 按语言标签选择对应的 Worker。
//
// 整个模块通过 loadMonaco() 动态 import,Monaco 及其语言服务只在
// 第一次打开代码编辑器时才加载,避免拖慢应用启动。

// 注意:monaco-editor 0.56 的 exports map 要求子路径不带 esm/vs/ 前缀
// (monaco-editor/editor/editor.worker → esm/vs/editor/editor.worker.js)。
import EditorWorker from 'monaco-editor/editor/editor.worker?worker'
import TsWorker from 'monaco-editor/language/typescript/ts.worker?worker'
import JsonWorker from 'monaco-editor/language/json/json.worker?worker'
import CssWorker from 'monaco-editor/language/css/css.worker?worker'
import HtmlWorker from 'monaco-editor/language/html/html.worker?worker'

export type Monaco = typeof import('monaco-editor')

let monacoPromise: Promise<Monaco> | null = null
let envReady = false

function setupEnvironment(): void {
  if (envReady) return
  envReady = true
  self.MonacoEnvironment = {
    getWorker(_workerId: string, label: string): Worker {
      switch (label) {
        case 'json':
          return new JsonWorker()
        case 'css':
        case 'scss':
        case 'less':
          return new CssWorker()
        case 'html':
        case 'handlebars':
        case 'razor':
          return new HtmlWorker()
        case 'typescript':
        case 'javascript':
          return new TsWorker()
        default:
          return new EditorWorker()
      }
    },
  }
}

// 与应用配色(styles.css 的 --editor-bg 等)保持一致的自定义主题
function defineThemes(monaco: Monaco): void {
  monaco.editor.defineTheme('spark-light', {
    base: 'vs',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': '#ffffff',
      'editor.lineHighlightBackground': '#f2f5fa',
      'editor.lineHighlightBorder': '#00000000',
      'editorCursor.foreground': '#2b3040',
      'editorLineNumber.foreground': '#8a91a3',
      'editorLineNumber.activeForeground': '#2b3040',
      'editorIndentGuide.background': '#eef1f7',
      'editorIndentGuide.activeBackground': '#d5dae5',
      'editor.selectionBackground': '#cfe0ff',
    },
  })
  monaco.editor.defineTheme('spark-dark', {
    base: 'vs-dark',
    inherit: true,
    rules: [],
    colors: {
      'editor.background': '#1a1d24',
      'editor.lineHighlightBackground': '#232733',
      'editor.lineHighlightBorder': '#00000000',
      'editorLineNumber.foreground': '#5c6475',
      'editorLineNumber.activeForeground': '#c9cede',
      'editorIndentGuide.background': '#2a2e37',
      'editorIndentGuide.activeBackground': '#3a4050',
    },
  })
}

/**
 * 懒加载 Monaco。返回同一个 Promise,多处同时调用只会加载一次。
 */
export function loadMonaco(): Promise<Monaco> {
  if (!monacoPromise) {
    monacoPromise = import('monaco-editor').then((m) => {
      setupEnvironment()
      defineThemes(m)
      return m
    })
  }
  return monacoPromise
}

/**
 * 根据明暗主题返回对应的 Monaco 主题名。
 */
export function monacoTheme(dark: boolean): string {
  return dark ? 'spark-dark' : 'spark-light'
}
