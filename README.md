# gosub

gosub 是一个本地代理订阅管理服务。当前技术栈为 Gin + GORM + templ + htmx，SQLite 只保存订阅数据。

## 功能

- 订阅分发：访问 SQLite 中保存的订阅路径时返回 Base64 编码后的节点列表。
- 管理后台：登录后可编辑订阅路径和订阅节点。
- 配置文件：`config.json` 管理 host、port、admin、password、TLS。
- 本地存储：`gosub.db` 保存订阅路径和节点列表。
- 暗色模式：前端默认跟随系统设置。

## 目录结构

```txt
gosub/
├─ main.go                    # 启动、配置初始化、启动信息输出
├─ server.go                  # Gin 路由、API、htmx 表单处理
├─ views/                     # templ 页面
├─ assets/                    # CSS
├─ lib/                       # 配置、SQLite/GORM store、session、订阅编码
├─ .github/workflows/         # push 构建产物与 tag release
├─ build.ps1                  # Windows 本地构建脚本
├─ config.example.json        # 配置模板
└─ go.mod
```

## 配置与数据

运行时文件：

- `config.json`：运行配置，包含监听地址、端口、管理员账号密码、TLS 配置。
- `gosub.db`：SQLite 数据库，保存订阅路径和订阅节点。

启动逻辑：

1. 如果 `config.json` 不存在，程序会生成默认配置并继续启动。
2. 如果 `gosub.db` 不存在，程序会创建数据库并生成随机订阅路径。
3. 每次启动都会在控制台输出管理后台 URL、订阅 URL、订阅 path、管理员账号密码、监听地址和 TLS 状态。

`config.json` 中的旧 `settings.path` 和 `nodes` 字段会被忽略。订阅路径和节点请在管理后台修改，或通过 API 写入 SQLite。

## 启动

```powershell
go run .
```

启动后访问：

- `/`：管理后台
- `/login`：登录页
- `{subscription_path}`：订阅地址
- `/api/*`：JSON API

## API

所有管理 API 都使用 `session_id` Cookie 鉴权。

- `POST /api/login`：表单参数 `username`、`password`
- `GET /api/session`
- `POST /api/logout`
- `GET /api/config`
- `POST /api/change_uri`：表单参数 `uri`，写入 SQLite 中的订阅路径
- `POST /api/change_nodes`：可重复表单参数 `nodes`
- `GET {subscription_path}`：返回 Base64 订阅文本

## 构建

Windows:

```powershell
.\build.ps1
```

通用:

```bash
go run github.com/a-h/templ/cmd/templ generate
go build -trimpath -ldflags "-s -w" -o gosub .
```

## CI/CD

- `.github/workflows/cd-release.yml`：每次 push 到分支都会运行测试、构建 linux/windows/darwin 的 amd64/arm64 产物，并上传 Actions artifacts。
- `.github/workflows/release-tag.yml`：push `v*` tag 时构建同样的产物，并发布 GitHub Release。

SQLite driver 使用纯 Go 实现，CI 中保持 `CGO_ENABLED=0` 跨平台构建。
