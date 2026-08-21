// Wails 绑定集中出口：前端所有对 Go 服务的调用都从这里导入。
// 绑定由 `wails3 generate bindings -ts -i` 生成（TS 接口版），勿手改。
import { TerminalService } from '../../bindings/changeme/app/service/terminal'
import { SFTPFileService } from '../../bindings/changeme/app/service/sftp'
import { FTPFileService } from '../../bindings/changeme/app/service/ftp'
import { ConnService } from '../../bindings/changeme/app/service/connections'
import { CustomCommandService } from '../../bindings/changeme/app/service/customcmd'
import { DocumentService } from '../../bindings/changeme/app/service/documents'
import { FavoriteService } from '../../bindings/changeme/app/service/favorites'
import { SettingsService } from '../../bindings/changeme/app/service/settings'
import { SiteService } from '../../bindings/changeme/app/service/sites'
import { DatabaseService } from '../../bindings/changeme/app/service/databases'
import { LocalService } from '../../bindings/changeme/app/service/local'
import { HostKeyService } from '../../bindings/changeme/app/service/hostkeys'
import { SshConfigService } from '../../bindings/changeme/app/service/sshconfig'
import { UpdateService } from '../../bindings/changeme/app/service/update'

import type {
  ConnectOptions,
  ServerInfo,
  ProcessInfo,
  SessionClosed,
} from '../../bindings/changeme/app/service/types/models'
import type {
  SavedConnection,
  CustomCommand,
  Favorite,
  ConnectionGroup,
  DocNode,
  Site,
  SiteLink,
  SiteAccount,
  SiteFolder,
} from '../../bindings/changeme/app/model/models'
import type { HostKeyInfo, HostKeyStatus } from '../../bindings/changeme/app/service/hostkeys/models'
import type { DatabaseConfig } from '../../bindings/changeme/app/service/databases/models'
import type { SshHost, ImportResult } from '../../bindings/changeme/app/service/sshconfig/models'
import type { DedupResult } from '../../bindings/changeme/app/service/connections/models'

export {
  TerminalService,
  SFTPFileService,
  FTPFileService,
  ConnService,
  CustomCommandService,
  DocumentService,
  FavoriteService,
  SettingsService,
  SiteService,
  DatabaseService,
  LocalService,
  HostKeyService,
  SshConfigService,
  UpdateService,
}
export type {
  ConnectOptions,
  SavedConnection,
  CustomCommand,
  Favorite,
  ConnectionGroup,
  DocNode,
  Site,
  SiteLink,
  SiteAccount,
  SiteFolder,
  HostKeyInfo,
  HostKeyStatus,
  ServerInfo,
  ProcessInfo,
  SessionClosed,
  DatabaseConfig,
  SshHost,
  ImportResult,
  DedupResult,
}

// 绑定模型为接口且字段全必填，这里提供带默认值的工厂函数便于构造。
export function makeConnectOptions(partial: Partial<ConnectOptions> = {}): ConnectOptions {
  return {
    host: '',
    port: 22,
    username: '',
    password: '',
    useKey: false,
    privateKey: '',
    passphrase: '',
    rows: 24,
    cols: 80,
    shell: '',
    tls: false,
    insecure: false,
    defaultDir: '',
    sessionId: '',
    ...partial,
  }
}

export function makeSavedConnection(partial: Partial<SavedConnection> = {}): SavedConnection {
  return {
    id: 0,
    name: '',
    group: '',
    type: 'ssh',
    host: '',
    port: 22,
    username: '',
    password: '',
    useKey: false,
    privateKey: '',
    passphrase: '',
    defaultDir: '',
    tls: false,
    createdAt: '',
    updatedAt: '',
    ...partial,
  }
}

export function makeCustomCommand(partial: Partial<CustomCommand> = {}): CustomCommand {
  return {
    id: 0,
    name: '',
    command: '',
    createdAt: '',
    updatedAt: '',
    ...partial,
  }
}

export function makeFavorite(partial: Partial<Favorite> = {}): Favorite {
  return {
    id: 0,
    kind: 'remote',
    connectionId: 0,
    path: '',
    createdAt: '',
    updatedAt: '',
    ...partial,
  }
}

export function makeDatabaseConfig(partial: Partial<DatabaseConfig> = {}): DatabaseConfig {
  return {
    dialect: 'sqlite',
    host: '',
    port: 0,
    username: '',
    password: '',
    database: '',
    params: '',
    syncKey: '',
    ...partial,
  }
}

// 后端事件名
export const EVENTS = {
  terminalOutput: 'terminal:output',
  terminalExit: 'terminal:exit',
  transferProgress: 'transfer:progress',
  sessionClosed: 'session:closed',
} as const
