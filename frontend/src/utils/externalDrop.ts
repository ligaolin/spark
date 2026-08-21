// Wails v3 原生文件拖放（EnableFileDrop）的转发注册表。
//
// 后端把拖入文件的「绝对路径 + 落点坐标」通过 files:dropped 事件发到前端
// （见 main.go 中 WindowFilesDropped 的监听），这里按坐标找到对应的文件面板
// 并触发其上传处理。相比浏览器内置的 dataTransfer.files（拿不到路径、目录
// 不可靠），原生拖放能拿到完整绝对路径且支持拖入整个目录。
import { Events } from '@wailsio/runtime'

interface DropPanel {
  el: HTMLElement
  handler: (paths: string[]) => void
}

const panels: DropPanel[] = []
let bound = false

// registerDropPanel 注册一个接受外部文件拖放的面板（如远程文件面板），
// 返回取消注册函数。handler 收到拖入的绝对路径列表（文件或目录）。
export function registerDropPanel(el: HTMLElement, handler: (paths: string[]) => void): () => void {
  const entry = { el, handler }
  panels.push(entry)
  bind()
  return () => {
    const i = panels.indexOf(entry)
    if (i >= 0) panels.splice(i, 1)
  }
}

function bind() {
  if (bound) return
  bound = true
  Events.On('files:dropped', (evt: any) => {
    const data = evt?.data
    if (!data || !Array.isArray(data.filenames) || data.filenames.length === 0) return
    const x = Number(data.x) || 0
    const y = Number(data.y) || 0
    const target = document.elementFromPoint(x, y) as HTMLElement | null
    if (!target) return
    // 取包含落点的最上层（最后注册的）面板
    for (let i = panels.length - 1; i >= 0; i--) {
      const p = panels[i]
      if (p.el === target || p.el.contains(target)) {
        p.handler(data.filenames)
        return
      }
    }
  })
}
