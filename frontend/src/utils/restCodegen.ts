// 把当前请求生成为 cURL / Python / JavaScript / Go 代码。纯函数。

import { buildCurl } from './restCurl'

export type CodeLanguage = 'curl' | 'curl-single' | 'curl-cmd' | 'curl-ps' | 'python' | 'javascript' | 'go'

export interface CodegenRequest {
    url: string
    method: string
    headers: Record<string, string>
    body: string
}

export const CODE_LANGUAGES: { label: string; value: CodeLanguage }[] = [
    { label: 'cURL（Bash/Linux）', value: 'curl' },
    { label: 'cURL（CMD）', value: 'curl-cmd' },
    { label: 'cURL（PowerShell）', value: 'curl-ps' },
    { label: 'cURL（单行）', value: 'curl-single' },
    { label: 'Python（requests）', value: 'python' },
    { label: 'JavaScript（fetch）', value: 'javascript' },
    { label: 'Go（net/http）', value: 'go' },
]

const TITLES: Record<CodeLanguage, string> = {
    curl: '生成 cURL 命令（Bash/Linux · 独立行）',
    'curl-single': '生成 cURL 命令（单行）',
    'curl-cmd': '生成 cURL 命令（CMD · 独立行）',
    'curl-ps': '生成 cURL 命令（PowerShell · 独立行）',
    python: '生成 Python 代码',
    javascript: '生成 JavaScript 代码',
    go: '生成 Go 代码',
}

const FILENAMES: Partial<Record<CodeLanguage, string>> = {
    python: 'request.py',
    javascript: 'request.js',
    go: 'request.go',
}

export function genDialogTitle(lang: CodeLanguage): string {
    return TITLES[lang] || '生成代码'
}

export function genEditorFilename(lang: CodeLanguage): string {
    return FILENAMES[lang] || 'curl.sh'
}

export function jsQuote(s: string): string {
    return JSON.stringify(s)
}

export function buildPython(req: CodegenRequest): string {
    const { url, body } = req
    if (!url) return ''

    const method = req.method.toUpperCase()
    const headers = req.headers

    const lines: string[] = ['import requests', '']
    lines.push(`url = ${jsQuote(url)}`)
    if (Object.keys(headers).length > 0) {
        const headerLines = Object.entries(headers)
            .filter(([k]) => k !== 'User-Agent')
            .map(([k, v]) => `    ${jsQuote(k)}: ${jsQuote(v)}`)
        lines.push(`headers = {`)
        lines.push(headerLines.join(',\n'))
        lines.push(`}`)
    }
    if (body) {
        lines.push(`data = ${jsQuote(body)}`)
    }

    const args: string[] = ['url']
    if (Object.keys(headers).filter(k => k !== 'User-Agent').length > 0) args.push('headers=headers')
    if (body) args.push('data=data')

    const methodLower = method.toLowerCase()
    lines.push(`response = requests.${methodLower}(${args.join(', ')})`)
    lines.push('print(response.text)')

    return lines.join('\n')
}

export function buildJavaScript(req: CodegenRequest): string {
    const { url, body } = req
    if (!url) return ''

    const method = req.method.toUpperCase()
    const headers = req.headers

    const filteredHeaders = Object.entries(headers).filter(([k]) => k !== 'User-Agent')
    const hasHeaders = filteredHeaders.length > 0

    const lines: string[] = []
    lines.push(`const url = ${jsQuote(url)};`)
    lines.push('')
    lines.push(`const options = {`)
    lines.push(`  method: ${jsQuote(method)},`)
    if (hasHeaders) {
        lines.push('  headers: {')
        filteredHeaders.forEach(([k, v], i) => {
            lines.push(`    ${jsQuote(k)}: ${jsQuote(v)}` + (i < filteredHeaders.length - 1 ? ',' : ''))
        })
        lines.push('  },')
    }
    if (body) {
        lines.push(`  body: ${jsQuote(body)},`)
    }
    lines.push('};')
    lines.push('')
    lines.push('fetch(url, options)')
    lines.push('  .then(res => res.json())')
    lines.push('  .then(data => console.log(data));')

    return lines.join('\n')
}

export function buildGo(req: CodegenRequest): string {
    const { url, body } = req
    if (!url) return ''

    const method = req.method.toUpperCase()
    const headers = req.headers
    const hasBody = body.length > 0

    const lines: string[] = [
        'package main',
        '',
        'import (',
    ]
    if (hasBody) {
        lines.push('    "fmt"', '    "io"', '    "net/http"', '    "strings"')
    } else {
        lines.push('    "fmt"', '    "io"', '    "net/http"')
    }
    lines.push(')', '')
    lines.push('func main() {')
    lines.push(`    url := ${jsQuote(url)}`)
    lines.push(`    method := ${jsQuote(method)}`)
    lines.push('')

    if (hasBody) {
        lines.push(`    payload := strings.NewReader(${jsQuote(body)})`)
        lines.push(`    req, err := http.NewRequest(method, url, payload)`)
    } else {
        lines.push(`    req, err := http.NewRequest(method, url, nil)`)
    }
    lines.push('    if err != nil {')
    lines.push('        fmt.Println(err)')
    lines.push('        return')
    lines.push('    }')
    lines.push('')

    for (const [k, v] of Object.entries(headers)) {
        if (k === 'User-Agent') continue
        lines.push(`    req.Header.Add(${jsQuote(k)}, ${jsQuote(v)})`)
    }
    lines.push('')
    lines.push('    res, err := http.DefaultClient.Do(req)')
    lines.push('    if err != nil {')
    lines.push('        fmt.Println(err)')
    lines.push('        return')
    lines.push('    }')
    lines.push('    defer res.Body.Close()')
    lines.push('')
    lines.push('    body, err := io.ReadAll(res.Body)')
    lines.push('    if err != nil {')
    lines.push('        fmt.Println(err)')
    lines.push('        return')
    lines.push('    }')
    lines.push('    fmt.Println(string(body))')
    lines.push('}')

    return lines.join('\n')
}

export function buildCode(lang: CodeLanguage, req: CodegenRequest): string {
    switch (lang) {
        case 'curl':
            return buildCurl(req.url, req.method, req.headers, req.body, '\\', 'multiline')
        case 'curl-single':
            return buildCurl(req.url, req.method, req.headers, req.body, '\\', 'single')
        case 'curl-cmd':
            return buildCurl(req.url, req.method, req.headers, req.body, '^', 'multiline')
        case 'curl-ps':
            return buildCurl(req.url, req.method, req.headers, req.body, '`', 'multiline')
        case 'python':
            return buildPython(req)
        case 'javascript':
            return buildJavaScript(req)
        case 'go':
            return buildGo(req)
    }
    return ''
}