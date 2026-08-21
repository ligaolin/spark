// 文档类型按文件扩展名判定（与后端 documents/service.go 的 kindForName 保持一致）。
// 以后新增类型：在这里加一条扩展名映射 + 在 DocumentsView 增加对应编辑器的
// 渲染分支即可（如 ".csv" → "csv"、".json" → "json"）。
const KIND_BY_EXT: Record<string, string> = {
    '.md': 'md',
    '.markdown': 'md',
}

// kindForName 根据文件名（扩展名，大小写不敏感）返回文档类型；无匹配时返回 "text"。
export function kindForName(name: string): string {
    const lower = (name || '').toLowerCase()
    for (const ext in KIND_BY_EXT) {
        if (lower.endsWith(ext)) return KIND_BY_EXT[ext]
    }
    return 'text'
}
