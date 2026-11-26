# Spark 终端

一款基于 Wails v3 + Vue 3 构建的跨平台桌面终端工具，把 SSH、文件管理和日常服务器运维放在同一个应用里。

## 功能

- **SSH 终端**：多标签、密码 / 私钥登录、跳板机多级连接、主机密钥校验（known_hosts）
- **SFTP 文件管理**：拖拽上传、断点续传、目录收藏、远程搜索与编辑器、本地 ↔ 远程镜像同步、服务器间直传
- **FTP / FTPS**：独立文件管理页，常用操作一应俱全
- **服务器面板**：主机信息、进程管理、网络与监听端口、端口转发 / SOCKS5 代理
- **AI 助手**：自带 API Key（兼容 OpenAI 接口），可在服务器上执行命令、多步完成运维任务
- **连接管理**：分组与搜索、多服务器批量执行命令、SSH 公钥生成与一键部署
- **文档与站点**：Markdown / 代码文档编辑；站点链接、账号密码管理与内嵌浏览器
- **REST 调试**：发送 HTTP 请求、请求历史回填
- **数据本地化**：默认 SQLite 存储，凭据 AES-256-GCM 加密；也可切换到远程 MySQL / PostgreSQL / SQL Server / Oracle 实现多机同步

## 下载

前往 [Releases](https://github.com/ligaolin/spark/releases) 下载对应平台的安装包。

## 从源码构建

需要 Go 1.24+、Node.js 18+ 与 [wails3 CLI](https://v3.wails.io/)。

```bash
cd frontend && npm install && cd ..
wails3 task dev     # 开发模式
wails3 task build   # 生产构建
```

## 许可

[MIT](LICENSE)