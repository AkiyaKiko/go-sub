# gosub

一个用 Go + React 实现的代理订阅管理系统。  
后端提供订阅内容分发和管理 API，前端提供登录后的管理界面（修改订阅路径、编辑全部节点连接）。

## 1. 功能概览

- 订阅分发：按 `config.json` 里的 `settings.path` 暴露订阅地址。
- 节点编码：将 `nodes` 按行拼接后做 Base64 输出。
- 管理登录：用户名密码登录，基于 `session_id` Cookie 会话鉴权。
- 配置管理：登录后可读取当前配置、修改订阅路径、修改节点列表。
- 配置持久化：管理修改会写回 `config.json`。
- 单文件部署：前端 `web/dist` 可通过 Go `embed` 打包进单个可执行文件。

## 2. 目录结构

```txt
gosub/
├─ web_embed.go           # 将 web/dist 内嵌进可执行文件
├─ build.ps1              # 一键打包脚本（先构建前端再构建 Go）
├─ main.go                # 后端入口
├─ config.json            # 运行配置
├─ lib/                   # 后端核心逻辑（API、会话、配置读写）
└─ web/                   # React 管理前端（Vite）
```

## 3. 环境要求

- Go：`go.mod` 当前声明 `go 1.25.6`
- Node.js：建议 24+
- pnpm：用于前端依赖管理

## 4. 配置文件说明（config.json）

首次使用建议先复制模板：

```powershell
Copy-Item config.example.json config.json
```

示例：

```json
{
  "settings": {
    "host": "0.0.0.0",
    "port": 12345,
    "path": "/cdJkuId",
    "admin": "admin",
    "password": "password",
    "tls": {
      "enabled": false,
      "certFile": "",
      "keyFile": ""
    }
  },
  "nodes": [
    "vless://example.com:443?path=/abc&tls=1",
    "vmess://example.com:443?path=/abc&tls=1",
    "ss://example.com:443?path=/abc&tls=1"
  ]
}
```

字段说明：

- `settings.host`：监听地址，仅允许 `localhost` / `127.0.0.1` / `0.0.0.0` / `::` / `::1`
- `settings.port`：监听端口
- `settings.path`：订阅路径（必须以 `/` 开头）
- `settings.admin`：管理账号
- `settings.password`：管理密码
- `settings.tls.enabled`：是否启用 HTTPS
- `settings.tls.certFile` / `keyFile`：启用 TLS 时必须填写证书路径
- `nodes`：订阅节点列表（每项一条连接）

注意：

- 如果 `config.json` 不存在，后端会自动生成默认文件并退出一次，需先编辑后再重启。
- 请务必修改默认管理员密码。
- 仓库中建议只提交 `config.example.json`，不要提交真实 `config.json`。

## 5. 启动后端

在项目根目录执行：

```powershell
go run .
```

启动成功后，日志会打印类似：

- `Starting server at http://HOST:PORT/PATH`
- 或 `Starting server at https://HOST:PORT/PATH`

订阅地址即：`http(s)://host:port + settings.path`

同时：

- 访问 `/` 会返回内嵌的管理前端页面
- 访问 `/api/*` 为管理 API
- 访问 `settings.path` 返回订阅 Base64 内容

## 6. 启动管理前端（开发模式）

进入前端目录并安装依赖：

```powershell
cd web
pnpm install
pnpm dev
```

说明：

- 前端开发服务器默认通过 `vite.config.ts` 将 `/api` 代理到 `http://127.0.0.1:8080`。
- 如果后端不是跑在 `127.0.0.1:8080`，请修改 `web/vite.config.ts` 的 `server.proxy`。

## 7. 管理前端使用流程

1. 打开前端页面，输入 `config.json` 的 `settings.admin` / `settings.password` 登录。
2. 登录后会自动读取当前配置：
   - 服务地址、TLS 状态、完整订阅 URL
   - 当前节点总数
3. 修改“订阅路径”并保存。
4. 在“全部订阅连接”文本框按行编辑节点并保存。
5. 退出登录可清除当前会话。

## 8. API 说明

所有管理 API 均在同一服务下，登录后通过 Cookie 鉴权。

### 8.1 登录与会话

- `POST /api/login`
  - 表单参数：`username`、`password`
  - 成功：`200`，并设置 `session_id` Cookie
- `GET /api/session`
  - 需登录
  - 成功：`200`，返回当前登录状态和 `user_id`
- `POST /api/logout`
  - 需登录
  - 成功：`200`，清理 session

### 8.2 管理配置

- `GET /api/config`
  - 需登录
  - 返回当前 `settings`、`nodes`、`subscription_url`
- `POST /api/change_uri`
  - 需登录
  - 表单参数：`uri`（必须以 `/` 开头）
- `POST /api/change_nodes`
  - 需登录
  - 表单参数：`nodes`（可重复传多次）

### 8.3 订阅分发

- `GET {settings.path}`
  - 返回 Base64 文本（由 `nodes` 按换行拼接后编码）

## 9. API 调用示例（curl）

登录并保存 Cookie：

```bash
curl -i -c cookie.txt -X POST "http://127.0.0.1:12345/api/login" \
  -d "username=admin" \
  -d "password=password"
```

读取管理配置：

```bash
curl -b cookie.txt "http://127.0.0.1:12345/api/config"
```

修改订阅路径：

```bash
curl -i -b cookie.txt -X POST "http://127.0.0.1:12345/api/change_uri" \
  -d "uri=/new-sub-path"
```

修改节点列表：

```bash
curl -i -b cookie.txt -X POST "http://127.0.0.1:12345/api/change_nodes" \
  -d "nodes=vless://node-1" \
  -d "nodes=vmess://node-2"
```

退出登录：

```bash
curl -i -b cookie.txt -X POST "http://127.0.0.1:12345/api/logout"
```

## 10. 打包为单个可执行文件（含前端）

方式 A：一键脚本（Windows PowerShell）

```powershell
.\build.ps1
```

执行后会自动：

1. 构建前端 `web/dist`
2. 使用 Go 编译 `gosub.exe`（已内嵌前端资源）

方式 B：手动执行

```powershell
cd web
pnpm build
cd ..
go build -trimpath -ldflags "-s -w" -o gosub.exe .
```

说明：

- `web_embed.go` 使用 `//go:embed web/dist`，所以 **Go 编译前必须先存在 `web/dist`**。
- 最终只需分发 `gosub.exe` + `config.json` 两个文件即可运行。

## 11. 常见问题

- 登录成功但前端仍未登录：
  - 检查是否请求到了正确后端端口。
  - 检查浏览器请求是否带上 Cookie（前端已使用 `credentials: include`）。
- 前端调用接口失败（开发模式）：
  - 检查 `web/vite.config.ts` 里的代理目标是否与后端实际地址一致。
- 配置改了但重启后丢失：
  - 确认进程对 `config.json` 有写权限。
- `go build` 报嵌入资源相关错误：
  - 先执行 `cd web && pnpm build`，确保 `web/dist` 已生成。

## 12. 生产环境建议

- 启用 TLS，避免明文传输管理密码和会话 Cookie。
- 将 `admin/password` 设为强密码，并限制管理端访问来源。
- 在反向代理层增加访问日志、限流和基础防护。
