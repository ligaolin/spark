// TextMate 语法高亮支持。
//
// 内置的 .tmLanguage(JSON / plist 格式)语法包通过 ?raw 打进构建产物,
// 运行时用 vscode-oniguruma(WASM 随包加载) + vscode-textmate 完成分词,
// 再注册为 Monaco 语言,由 vs / vs-dark 等主题自带的 TextMate scope 规则着色。
//
// 新增语言:把语法文件放进 src/assets/grammars,在 GRAMMARS 与 LANGS
// 各加一条即可(scopeName 与语法文件里的 scopeName 一致)。

import { Registry, parseRawGrammar, INITIAL, type IGrammar, type StateStack } from 'vscode-textmate'
import { loadWASM, OnigScanner, OnigString } from 'vscode-oniguruma'
import onigWasmUrl from 'vscode-oniguruma/release/onig.wasm?url'
import type { languages } from 'monaco-editor'
import type { Monaco } from './monaco'

import vueGrammar from '../assets/grammars/vue.tmLanguage.json?raw'
import tomlGrammar from '../assets/grammars/toml.tmLanguage?raw'
import nginxGrammar from '../assets/grammars/nginx.tmLanguage?raw'

interface GrammarEntry {
  scopeName: string
  /** 语法文件内容(JSON 或 plist 文本) */
  content: string
  /** 语法文件名;parseRawGrammar 依扩展名判断格式(.json → JSON,其余 → plist) */
  filePath: string
}

interface LangEntry {
  id: string
  scopeName: string
  aliases?: string[]
  extensions?: string[]
  filenames?: string[]
}

const GRAMMARS: GrammarEntry[] = [
  { scopeName: 'source.vue', content: vueGrammar, filePath: 'vue.tmLanguage.json' },
  { scopeName: 'source.toml', content: tomlGrammar, filePath: 'toml.tmLanguage' },
  { scopeName: 'source.nginx', content: nginxGrammar, filePath: 'nginx.tmLanguage' },
]

const LANGS: LangEntry[] = [
  { id: 'vue', scopeName: 'source.vue', aliases: ['Vue'], extensions: ['.vue'] },
  { id: 'toml', scopeName: 'source.toml', aliases: ['TOML'], extensions: ['.toml'] },
  {
    id: 'nginx',
    scopeName: 'source.nginx',
    aliases: ['Nginx'],
    extensions: ['.nginx', '.nginxconf'],
    filenames: ['nginx.conf'],
  },
]

class TokenizerState implements languages.IState {
  readonly ruleStack: StateStack

  constructor(ruleStack: StateStack) {
    this.ruleStack = ruleStack
  }

  clone(): languages.IState {
    return new TokenizerState(this.ruleStack.clone())
  }

  equals(other: languages.IState): boolean {
    return other instanceof TokenizerState && this.ruleStack.equals(other.ruleStack)
  }
}

function createTokenizationSupport(grammar: IGrammar): languages.TokensProvider {
  return {
    getInitialState: () => new TokenizerState(INITIAL),
    tokenize(line: string, state: languages.IState): languages.ILineTokens {
      const result = grammar.tokenizeLine(line, (state as TokenizerState).ruleStack)
      return {
        tokens: result.tokens.map((t) => ({
          startIndex: t.startIndex,
          scopes: t.scopes.join(' '),
        })),
        endState: new TokenizerState(result.ruleStack),
      }
    },
  }
}

let registryPromise: Promise<void> | null = null

async function setup(monaco: Monaco): Promise<void> {
  // onig.wasm 由 Vite 按 ?url 打成本地资源,用 fetch 取回后交给 oniguruma
  await loadWASM(await fetch(onigWasmUrl))

  const registry = new Registry({
    onigLib: Promise.resolve({
      createOnigScanner: (patterns) => new OnigScanner(patterns),
      createOnigString: (str) => new OnigString(str),
    }),
    loadGrammar: (scopeName) => {
      const entry = GRAMMARS.find((g) => g.scopeName === scopeName)
      return Promise.resolve(entry ? parseRawGrammar(entry.content, entry.filePath) : null)
    },
  })

  for (const lang of LANGS) {
    const grammar = await registry.loadGrammar(lang.scopeName)
    if (!grammar) continue
    monaco.languages.register({
      id: lang.id,
      aliases: lang.aliases,
      extensions: lang.extensions,
      filenames: lang.filenames,
    })
    monaco.languages.setTokensProvider(lang.id, createTokenizationSupport(grammar))
  }
}

/**
 * 注册 TextMate 语法高亮。幂等:整个应用生命周期只执行一次。
 * 失败(如 WASM 加载异常)不影响编辑,只会缺少对应语言的高亮。
 */
export function registerTextMateGrammars(monaco: Monaco): Promise<void> {
  if (!registryPromise) {
    registryPromise = setup(monaco).catch((e) => {
      // 出错后允许下次重试
      registryPromise = null
      throw e
    })
  }
  return registryPromise
}
