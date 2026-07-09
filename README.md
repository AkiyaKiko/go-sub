# gosub

gosub 是一个本地代理订阅管理服务。当前技术栈为 Gin + GORM + Go 标准库 `html/template` + 原生 JavaScript；运行配置保存在 `config.json`，订阅路径、节点列表和路由规则保存在 SQLite。

## 功能

- 订阅分发：访问保存的订阅路径时，返回 Base64 编码后的节点列表。
- 管理后台：登录后可编辑订阅路径、节点列表和路由规则。
- 路由规则订阅：可维护多条独立 JSON 规则，每条规则都有独立访问 URL。
- 原生页面：使用 Go `html/template` 和原生 JavaScript，不依赖 templ/htmx。
- 本地存储：使用 GORM 管理 SQLite 表结构和 CRUD。
- TLS 支持：启用 TLS 时支持同端口 HTTPS 服务和 HTTP 到 HTTPS 跳转。

## 项目结构

```txt
gosub/
├─ main.go                    # 程序入口：配置、数据库初始化、启动 HTTP/HTTPS 服务
├─ controller/
│  ├─ server.go               # Gin 实例、middleware、路由注册、静态资源输出
│  ├─ handler.go              # 页面、API、订阅输出、路由规则相关 handler
│  ├─ support.go              # 状态码、session 鉴权、模板渲染、安全响应头、同源校验
│  ├─ tls_redirect_server.go  # TLS 开启时同端口 HTTPS 与 HTTP 跳转逻辑
│  ├─ dynamic_route_test.go   # 路由规则动态访问测试
│  └─ security_test.go        # 同源写请求保护测试
├─ model/
│  └─ model.go                # GORM 模型：节点、订阅路径、路由规则
├─ lib/
│  ├─ config.go               # config.json 加载、解析、校验、鉴权比较、路径校验
│  ├─ store.go                # SQLite/GORM 初始化、迁移、节点/订阅/路由规则 CRUD
│  ├─ session.go              # 内存 session 存储和 session token 生成
│  ├─ random.go               # 加密随机字符串、公开路径、用户 ID 生成
│  ├─ b64enc.go               # 节点拼接和 Base64 订阅文本生成
│  └─ store_test.go           # 存储层和路径/JSON 校验测试
├─ assets/
│  ├─ app.css                 # 管理后台样式，通过 go:embed 打进二进制
│  └─ app.js                  # 原生 JavaScript 表单提交和路由规则下拉编辑
├─ template/
│  ├─ login.html              # 登录页
│  └─ dashboard.html          # 管理后台
├─ .github/workflows/
│  ├─ cd-release.yml          # push / 手动触发：运行 go test
│  └─ release-tag.yml         # v* tag：测试、跨平台构建并发布 GitHub Release
├─ build.ps1                  # Windows 本地构建脚本
├─ config.example.json        # 配置示例
├─ go.mod / go.sum            # Go 模块与依赖锁定
└─ README.md
```

根目录只保留 `main.go` 作为 Go 程序入口。相近职责代码已合并到单文件：controller 的页面/API/规则处理集中在 `handler.go`，支撑逻辑集中在 `support.go`，GORM 模型集中在 `model/model.go`。只有 TLS 启动、session 存储、随机工具、Base64 编码这类独立功能保留为单独文件。

运行后可能出现这些本地文件，它们不属于源码结构：

- `config.json`：运行配置，缺失时会自动生成。
- `gosub.db`、`gosub.db-*`：SQLite 数据库及临时文件。
- `gosub.exe`：Windows 本地构建产物。
- `.gocache/`：本地 Go 构建缓存。

## 数据流

1. `main.go` 设置配置路径，确保 `config.json` 存在，初始化 `gosub.db`，再创建 Gin 服务。
2. `lib/store.go` 使用 `model/model.go` 中的 GORM 模型自动迁移 SQLite 表，并确保至少存在一个订阅路径。
3. 管理后台和 API 请求进入 `controller`，通过 `lib/store.go` 读写订阅路径、节点列表和路由规则。
4. 访问订阅路径时，服务读取节点列表，拼接换行文本后返回 Base64 编码结果。
5. 访问任一路由规则路径时，服务返回该规则保存的 JSON 内容。

## 配置

首次启动如果没有 `config.json`，程序会生成默认配置并继续启动。默认监听 `127.0.0.1:8080`，管理员账号和密码会带随机片段，并且只会在首次生成配置时输出密码；后续启动日志会隐藏密码。

配置示例：

```json
{
  "settings": {
    "host": "127.0.0.1",
    "port": 12345,
    "admin": "admin",
    "password": "changeMePassword",
    "tls": {
      "enabled": false,
      "certFile": "",
      "keyFile": ""
    }
  }
}
```

旧版 `settings.path` 和 `nodes` 字段会被忽略。订阅路径、节点列表和路由规则请在管理后台修改，或通过 API 写入 SQLite。

## 启动

```powershell
go run .
```

启动后访问：

- `/`：管理后台
- `/login`：登录页
- `{subscription_path}`：订阅地址
- `{route_rule_path}`：路由规则 JSON 地址
- `/api/*`：JSON API

## API

除 `POST /api/login` 外，所有管理 API 都需要 `session_id` Cookie。

- `POST /api/login`：表单参数 `username`、`password`
- `GET /api/session`
- `POST /api/logout`
- `GET /api/config`
- `POST /api/change_uri`：表单参数 `uri`
- `POST /api/change_nodes`：可重复表单参数 `nodes`
- `GET {subscription_path}`：返回 Base64 订阅文本
- `GET {route_rule_path}`：返回对应路由规则 JSON

管理后台表单接口同样使用 `session_id` Cookie：

- `POST /subscription-path`：保存或随机生成订阅路径
- `POST /nodes`：保存节点列表
- `POST /route-rules`：新增路由规则，表单参数 `name`、`path`、`content`、`submit_action`
- `POST /route-rules/{id}`：修改路由规则
- `POST /route-rules/{id}/delete`：删除路由规则

## 安全性

已实现的保护：

- 管理接口使用 `session_id` Cookie 鉴权，Cookie 设置为 `HttpOnly` 和 `SameSite=Lax`；启用 TLS 时自动设置 `Secure`。
- 后台写请求会检查 `Origin` / `Referer`，跨源写入会被拒绝。
- 登录账号和密码比较使用 SHA-256 后的固定时间比较，减少时序泄露。
- `config.json` 和 `gosub.db` 会尽量设置为 `0600` 权限。
- 新生成的订阅路径和路由规则路径使用加密随机数，默认 16 位字母随机片段。
- 公开路径会拒绝空值、空白字符、查询串、fragment、过长路径和系统保留路径，例如 `/api`、`/assets`、`/login`、`/route-rules`。
- 路由规则 JSON 会校验格式并限制最大 512 KiB。
- 响应会设置 CSP、`X-Frame-Options`、`X-Content-Type-Options`、`Referrer-Policy`，非静态响应默认 `Cache-Control: no-store`。
- 启用 TLS 时返回 HSTS 头。

需要注意的边界：

- 订阅路径和路由规则路径是公开读取 URL，设计上不要求登录；安全性依赖随机路径的不可猜测性。不要使用容易猜到的路径。
- `config.json` 中的管理员密码仍是明文配置项，应只放在可信机器上，并限制文件访问权限。
- 如果把 `host` 改成 `0.0.0.0` 对外监听，请启用 TLS、使用高强度密码，并通过防火墙或反向代理限制访问面。
- 内存 session 会在程序重启后失效；这有利于减少持久化风险，但重启后需要重新登录。

## 构建

Windows:

```powershell
.\build.ps1
```

通用:

```bash
go build -trimpath -ldflags "-s -w" -o gosub .
```

## 测试

```powershell
go test ./...
```

CI 会在 push 和 release tag 流程中运行测试。SQLite driver 使用纯 Go 实现，便于 `CGO_ENABLED=0` 跨平台构建。
