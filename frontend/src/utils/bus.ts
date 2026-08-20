// 极简事件总线：用于跨组件触发动作（如全局快捷键 -> 终端视图行为）
type Handler = () => void

const handlers = new Map<string, Set<Handler>>()

export function on(name: string, fn: Handler): () => void {
  if (!handlers.has(name)) handlers.set(name, new Set())
  handlers.get(name)!.add(fn)
  return () => {
    handlers.get(name)?.delete(fn)
  }
}

export function emit(name: string) {
  handlers.get(name)?.forEach((fn) => fn())
}
