// AI 供应商配置：设置页（选供应商）和终端 AI 面板共用。
// 只保留供应商的标识/地址/默认模型；模型列表一律从供应商 /models 接口动态获取，
// 不再内置写死（避免过时 / 不准）。

export interface AIProvider {
  key: string
  label: string
  baseUrl: string
  model: string // 该供应商的默认模型
}

export const AI_PROVIDERS: AIProvider[] = [
  { key: 'openai', label: 'OpenAI', baseUrl: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
  { key: 'deepseek', label: 'DeepSeek', baseUrl: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
  { key: 'qwen', label: '通义千问（阿里云）', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen-plus' },
  { key: 'zhipu', label: '智谱 AI', baseUrl: 'https://open.bigmodel.cn/api/paas/v4', model: 'glm-4-flash' },
  { key: 'custom', label: '自定义', baseUrl: '', model: '' },
]

// 根据 baseUrl（去掉末尾 /）反推供应商；匹配不到返回 undefined（自定义地址）。
export function providerForBaseUrl(baseUrl: string): AIProvider | undefined {
  const cur = (baseUrl || '').replace(/\/+$/, '')
  return AI_PROVIDERS.find((p) => p.baseUrl && p.baseUrl.replace(/\/+$/, '') === cur)
}
