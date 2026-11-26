// curl 命令的解析与生成，以及跨组件共享的键值对类型。
// 纯函数，不依赖组件状态。

export interface KV {
    key: string
    value: string
    enabled?: boolean
}

export interface CurlParseResult {
    method: string
    url: string
    headers: KV[]
    body: string
    params: KV[]
    formParts: string[]
    urlencodedParts: string[]
}

export function safeDecode(s: string): string {
    try {
        return decodeURIComponent(s.replace(/\+/g, ' '))
    } catch {
        return s
    }
}

export function tokenize(s: string): string[] {
    const tokens: string[] = []
    let i = 0
    while (i < s.length) {
        while (i < s.length && /\s/.test(s[i])) i++
        if (i >= s.length) break

        if (s[i] === '$' && (s[i + 1] === "'" || s[i + 1] === '"')) {
            // $'...' ANSI-C 引用（Bash）：还原 \n / \t / \xNN 等转义
            const quote = s[i + 1]
            i += 2
            let val = ''
            while (i < s.length && s[i] !== quote) {
                if (s[i] === '\\' && i + 1 < s.length) {
                    const c = s[i + 1]
                    const simple: Record<string, string> = {
                        n: '\n',
                        t: '\t',
                        r: '\r',
                        '\\': '\\',
                        "'": "'",
                        '"': '"',
                        '0': '\0',
                    }
                    if (c in simple) {
                        val += simple[c]
                        i += 2
                        continue
                    }
                    if (c === 'x' && /^[0-9a-f]{2}/i.test(s.slice(i + 2, i + 4))) {
                        val += String.fromCharCode(parseInt(s.slice(i + 2, i + 4), 16))
                        i += 4
                        continue
                    }
                    val += c
                    i += 2
                    continue
                }
                val += s[i]
                i++
            }
            i++
            tokens.push(val)
        } else if (s[i] === "'") {
            i++
            let val = ''
            while (i < s.length && s[i] !== "'") {
                val += s[i]
                i++
            }
            i++
            tokens.push(val)
        } else if (s[i] === '"') {
            i++
            let val = ''
            while (i < s.length && s[i] !== '"') {
                if (s[i] === '\\' && i + 1 < s.length) {
                    val += s[i + 1]
                    i += 2
                } else {
                    val += s[i]
                    i++
                }
            }
            i++
            tokens.push(val)
        } else {
            let val = ''
            while (i < s.length && !/\s/.test(s[i])) {
                if (s[i] === '\\' && i + 1 < s.length) {
                    val += s[i + 1]
                    i += 2
                } else {
                    val += s[i]
                    i++
                }
            }
            tokens.push(val)
        }
    }
    return tokens
}

export function parseCurl(cmd: string): CurlParseResult {
    const result: CurlParseResult = {
        method: 'GET',
        url: '',
        headers: [],
        body: '',
        params: [],
        formParts: [],
        urlencodedParts: [],
    }

    // Bash 反斜杠续行 / CMD 脱字符 / PowerShell 反引号续行
    let s = cmd
        .replace(/\\(\r?\n|$)/g, ' ')
        .replace(/\^(\r?\n|$)/g, ' ')
        .replace(/`(\r?\n|$)/g, ' ')
    // 去掉命令行提示符与 curl 可执行名
    s = s.replace(/^\s*(?:\$\s+)?curl(?:\.exe)?\s+/i, '')

    const tokens = tokenize(s)
    const dataParts: string[] = []
    let methodSet = false
    let hasBody = false

    const pushHeader = (raw: string) => {
        const idx = raw.indexOf(':')
        if (idx > 0) {
            result.headers.push({
                key: raw.slice(0, idx).trim(),
                value: raw.slice(idx + 1).trim(),
            })
        }
    }

    // 取下一个参数，越界时返回空串
    const next = (i: number) => (i + 1 < tokens.length ? tokens[i + 1] : '')

    let i = 0
    while (i < tokens.length) {
        const t = tokens[i]

        if (t === '-X' || t === '--request') {
            const v = next(i)
            if (v) result.method = v.toUpperCase()
            methodSet = true
            i++
        } else if (/^-X./.test(t)) {
            // 紧凑写法 -XPOST
            result.method = t.slice(2).toUpperCase()
            methodSet = true
        } else if (t === '--url') {
            result.url = next(i)
            i++
        } else if (t === '-H' || t === '--header') {
            pushHeader(next(i))
            i++
        } else if (
            t === '-d' ||
            t === '--data' ||
            t === '--data-raw' ||
            t === '--data-binary' ||
            t === '--data-ascii'
        ) {
            const v = next(i)
            if (v) {
                dataParts.push(v)
                hasBody = true
            }
            i++
        } else if (t === '--data-urlencode') {
            const v = next(i)
            if (v) {
                result.urlencodedParts.push(v)
                dataParts.push(v)
                hasBody = true
            }
            i++
        } else if (t === '-F' || t === '--form' || t === '--form-string') {
            const v = next(i)
            if (v) {
                result.formParts.push(v)
                hasBody = true
            }
            i++
        } else if (t === '--json') {
            const v = next(i)
            if (v) {
                dataParts.push(v)
                hasBody = true
                if (!result.headers.some((h) => h.key.toLowerCase() === 'content-type')) {
                    result.headers.push({ key: 'Content-Type', value: 'application/json' })
                }
            }
            i++
        } else if (t === '-u' || t === '--user') {
            const v = next(i)
            if (v) result.headers.push({ key: 'Authorization', value: 'Basic ' + btoa(v) })
            i++
        } else if (t === '-A' || t === '--user-agent') {
            const v = next(i)
            if (v) result.headers.push({ key: 'User-Agent', value: v })
            i++
        } else if (t === '-G' || t === '--get') {
            result.method = 'GET'
            methodSet = true
        } else if (t === '-I' || t === '--head') {
            result.method = 'HEAD'
            methodSet = true
        } else if (t === '-b' || t === '--cookie') {
            const v = next(i)
            if (v) result.headers.push({ key: 'Cookie', value: v })
            i++
        } else if (t === '--compressed') {
            result.headers.push({ key: 'Accept-Encoding', value: 'gzip, deflate' })
        } else if (!t.startsWith('-') && !result.url) {
            result.url = t
        }

        i++
    }

    result.body = dataParts.join('&')
    // 带了请求体但没显式指定方法时，curl 默认使用 POST
    if (hasBody && !methodSet) result.method = 'POST'

    if (!result.url) {
        for (const tk of tokens) {
            if (/^https?:\/\//i.test(tk)) {
                result.url = tk
                break
            }
        }
    }

    if (result.url) {
        const qIdx = result.url.indexOf('?')
        if (qIdx >= 0) {
            const queryString = result.url.slice(qIdx + 1)
            result.url = result.url.slice(0, qIdx)
            if (queryString) {
                result.params = queryString.split('&').map((pair) => {
                    const eqIdx = pair.indexOf('=')
                    if (eqIdx >= 0) {
                        return {
                            key: decodeURIComponent(pair.slice(0, eqIdx)),
                            value: decodeURIComponent(pair.slice(eqIdx + 1)),
                        }
                    }
                    return { key: decodeURIComponent(pair), value: '' }
                })
            }
        }
    }

    return result
}

export function shellQuote(s: string, shell: 'bash' | 'cmd' | 'powershell' = 'bash'): string {
    if (/^[a-zA-Z0-9_\-./:?=#%]+$/.test(s)) return s
    if (shell === 'cmd') {
        if (!s.includes('"')) return `"${s}"`
        return `"${s.replace(/"/g, '""')}"`
    }
    if (shell === 'powershell') {
        if (!s.includes("'")) return `'${s}'`
        if (!s.includes('"')) return `"${s}"`
        return `@"` + "\n" + `${s}` + "\n" + `"@`
    }
    if (!s.includes("'")) return `'${s}'`
    if (!s.includes('"')) return `"${s}"`
    return `'${s.replace(/'/g, "'\\''")}'`
}

export function buildCurl(
    url: string,
    method: string,
    headers: Record<string, string>,
    body: string,
    continuation: string,
    style: 'single' | 'multiline',
): string {
    if (!url) return ''

    const shell: 'bash' | 'cmd' | 'powershell' =
        continuation === '^' ? 'cmd' : continuation === '`' ? 'powershell' : 'bash'
    const NL = continuation === '^' ? '\r\n' : '\n'

    const tokens: string[] = []
    tokens.push(shell === 'cmd' || shell === 'powershell' ? 'curl.exe' : 'curl')

    tokens.push(shellQuote(url, shell))

    const upper = method.toUpperCase()
    if (upper !== 'GET') {
        tokens.push('-X', shellQuote(upper, shell))
    }

    for (const [k, v] of Object.entries(headers)) {
        if (k === 'User-Agent') continue
        tokens.push('-H', shellQuote(`${k}: ${v}`, shell))
    }

    if (body) {
        tokens.push('-d', shellQuote(body, shell))
    }

    if (style === 'single') {
        return tokens.join(' ')
    }

    let firstLine = tokens[0] + ' ' + tokens[1]
    let flagStart = 2
    if (tokens.length > 2 && tokens[2] === '-X') {
        firstLine += ' ' + tokens[2] + ' ' + tokens[3]
        flagStart = 4
    }
    const lines: string[] = [firstLine]
    for (let i = flagStart; i < tokens.length; i += 2) {
        lines.push('  ' + tokens[i] + (i + 1 < tokens.length ? ' ' + tokens[i + 1] : ''))
    }
    return lines.map((l, idx) => (idx < lines.length - 1 ? l + ' ' + continuation : l)).join(NL)
}