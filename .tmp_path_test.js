// 从 fileBackend.ts 摘出的纯逻辑测试（与源码保持一致）
function parentDir(path, sep) {
  if (!path) return sep
  const s = path.includes('/') ? '/' : path.includes('\\') ? '\\' : sep
  if (/^[\\/]{2}/.test(path)) {
    const parts = path.replace(/[\\/]+/g, s).split(s).filter(Boolean)
    if (parts.length <= 2) return path.replace(/[\\/]+$/, '')
    return s + s + parts.slice(0, parts.length - 1).join(s)
  }
  const cleaned = path.replace(/[\\/]+/g, s).replace(new RegExp(`\\${s}+$`), '')
  if (!cleaned) return s
  const drive = cleaned.match(/^([A-Za-z]:)([\\/]?)(.*)$/)
  if (drive) {
    const prefix = drive[1]
    const rest = drive[3]
    if (!rest) return prefix + s
    const idx = rest.lastIndexOf(s)
    if (idx < 0) return prefix + s
    return prefix + s + rest.slice(0, idx)
  }
  if (cleaned.startsWith(s)) {
    const rest = cleaned.slice(1)
    const idx = rest.lastIndexOf(s)
    if (idx < 0) return s
    return s + rest.slice(0, idx)
  }
  const idx = cleaned.lastIndexOf(s)
  if (idx < 0) return sep
  return cleaned.slice(0, idx)
}

function joinPath(base, name, sep) {
  const s = base.includes('/') ? '/' : base.includes('\\') ? '\\' : sep
  const trimmed = base.replace(/[\\/]+$/, '')
  if (!trimmed) return `${s}${name}`
  return `${trimmed}${s}${name}`
}

// 期望: [输入, sep, 期望父目录, 期望是根]
const cases = [
  ['/home/user', '/', '/home', false],
  ['/home/user/', '/', '/home', false],
  ['/home', '/', '/', false],
  ['/', '/', '/', true],
  ['', '/', '/', true],
  ['a/b', '/', 'a', false],
  ['a', '/', '/', true],
  ['a/', '/', '/', true],
  ['/home//user', '/', '/home', false],
  ['//', '/', '/', true],
  ['C:\\Users\\foo', '/', 'C:\\Users', false],
  ['C:\\Users', '/', 'C:\\', false],
  ['C:\\', '/', 'C:\\', true],
  ['C:/Users/foo', '/', 'C:/Users', false],
  ['C:/Users', '/', 'C:/', false],
  ['C:/', '/', 'C:/', true],
  ['\\\\server\\share\\dir', '/', '\\\\server\\share', false],
  ['\\\\server\\share', '/', '\\\\server\\share', true],
  ['/C:/Users/97679', '/', '/C:/Users', false],
  ['/C:', '/', '/', false],
  ['/C:/Users', '/', '/C:', false],
  ['..', '/', '/', true],
  ['.', '/', '/', true],
]

let failed = 0
for (const [input, sep, expected, isRootExpected] of cases) {
  const got = parentDir(input, sep)
  const isRoot = got === input
  const ok = got === expected && isRoot === isRootExpected
  if (!ok) {
    failed++
    console.log(`FAIL ${JSON.stringify(input)} -> got ${JSON.stringify(got)} (root=${isRoot}), expected ${JSON.stringify(expected)} (root=${isRootExpected})`)
  } else {
    console.log(`ok   ${JSON.stringify(input)} -> ${JSON.stringify(got)}`)
  }
}

const joins = [
  ['/home', 'x', '/home/x'],
  ['/home/', 'x', '/home/x'],
  ['/', 'x', '/x'],
  ['', 'x', '/x'],
  ['C:\\Users\\foo', 'bar', 'C:\\Users\\foo\\bar'],
  ['C:\\', 'bar', 'C:\\bar'],
  ['\\\\server\\share', 'f', '\\\\server\\share\\f'],
  ['/home//', 'x', '/home/x'],
]
for (const [b, n, expected] of joins) {
  const got = joinPath(b, n, '/')
  if (got !== expected) {
    failed++
    console.log(`JOIN FAIL ${JSON.stringify(b)} + ${n} -> ${JSON.stringify(got)}, expected ${JSON.stringify(expected)}`)
  } else {
    console.log(`ok   join ${JSON.stringify(b)} + ${n} -> ${JSON.stringify(got)}`)
  }
}
console.log(failed === 0 ? 'ALL PASS' : `${failed} FAILURES`)
