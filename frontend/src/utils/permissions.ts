// POSIX 权限位与八进制互转，供「修改权限」对话框使用。
export interface PermBits {
  owner: { r: boolean; w: boolean; x: boolean }
  group: { r: boolean; w: boolean; x: boolean }
  public: { r: boolean; w: boolean; x: boolean }
}

export function emptyBits(): PermBits {
  return {
    owner: { r: false, w: false, x: false },
    group: { r: false, w: false, x: false },
    public: { r: false, w: false, x: false },
  }
}

// 解析 "drwxr-xr-x" 或 "755" / "0755"；无法识别时返回 755。
export function parseMode(mode?: string): PermBits {
  const bits = emptyBits()
  const raw = (mode || '').trim()
  if (/^[0-7]{3,4}$/.test(raw)) return octalToBits(parseInt(raw.slice(-3), 8))
  const rwx = raw.slice(-9)
  if (!/^[rwx-]{9}$/.test(rwx)) {
    bits.owner = { r: true, w: true, x: true }
    bits.group = { r: true, w: false, x: true }
    bits.public = { r: true, w: false, x: true }
    return bits
  }
  const groups: Array<keyof PermBits> = ['owner', 'group', 'public']
  groups.forEach((g, i) => {
    const part = rwx.slice(i * 3, i * 3 + 3)
    bits[g] = { r: part[0] === 'r', w: part[1] === 'w', x: part[2] === 'x' }
  })
  return bits
}

export function octalToBits(n: number): PermBits {
  const bits = emptyBits()
  const oct = (n & 0o777).toString(8).padStart(3, '0')
  const groups: Array<keyof PermBits> = ['owner', 'group', 'public']
  groups.forEach((g, i) => {
    const digit = parseInt(oct[i], 8)
    bits[g] = { r: (digit & 4) !== 0, w: (digit & 2) !== 0, x: (digit & 1) !== 0 }
  })
  return bits
}

export function bitsToOctal(bits: PermBits): number {
  const one = (b: { r: boolean; w: boolean; x: boolean }) =>
    (b.r ? 4 : 0) | (b.w ? 2 : 0) | (b.x ? 1 : 0)
  return (one(bits.owner) << 6) | (one(bits.group) << 3) | one(bits.public)
}

export function formatOctal(n: number): string {
  return (n & 0o777).toString(8).padStart(3, '0')
}
