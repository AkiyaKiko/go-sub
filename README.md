# gosub

gosub 是一个本地代理订阅管理服务。当前技术栈为 Gin + GORM + Go 标准库 `html/template` + 原生 JavaScript；运行配置保存在 `config.json`，订阅路径、节点列表和路由规则保存在 SQLite。

## 功能

- 订阅分发：访问保存的订阅路径时，返回 Base64 编码后的节点列表。
- sing-box 订阅：支持 VLESS、Shadowsocks、VMess、Trojan、Hysteria 1/2、TUIC v5、AnyTLS、HTTP(S) 和 SOCKS5 分享链接，本地生成独立的完整 sing-box JSON 配置，原 Xray URL 和 Base64 输出保持不变。
- sing-box 模式：默认生成手机 TUN / VPN 配置；后台下拉切换为桌面 Mixed 代理端口模式，设置持久化后立即作用于原订阅 URL。
- 管理后台：登录后可编辑订阅路径、节点列表和路由规则。
- 当前服务：展示所有路由规则的名称和订阅路径，新增、修改、删除后自动更新。
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
│  ├─ dashboard_test.go       # 本地 HTTP 登录和后台路由规则路径展示测试
│  ├─ singbox_test.go         # 双订阅 URL、Xray 字节兼容和撤销/错误隔离测试
│  ├─ dynamic_route_test.go   # 路由规则动态访问测试
│  └─ security_test.go        # 同源写请求保护测试
├─ model/
│  └─ model.go                # GORM 模型：节点、订阅路径、路由规则、sing-box 发布缓存
├─ lib/
│  ├─ config.go               # config.json 加载、解析、校验、模式设置原子保存、鉴权/路径校验
│  ├─ config_test.go          # 本地监听忽略 TLS 和其他监听地址的配置校验测试
│  ├─ store.go                # SQLite/GORM 初始化、迁移、节点/订阅/路由规则 CRUD
│  ├─ session.go              # 内存 session 存储和 session token 生成
│  ├─ random.go               # 加密随机字符串、公开路径、用户 ID 生成
│  ├─ b64enc.go               # 节点拼接和 Base64 订阅文本生成
│  ├─ singbox.go              # 分享链接解析、协议参数转换和 sing-box 配置组装
│  ├─ singbox_test.go         # 转换、缓存/并发和官方 sing-box check 测试
│  └─ store_test.go           # 存储层和路径/JSON 校验测试
├─ assets/
│  ├─ app.css                 # 管理后台样式，通过 go:embed 打进二进制
│  └─ app.js                  # 原生 JavaScript 表单提交和路由规则下拉编辑
├─ template/
│  ├─ login.html              # 登录页
│  └─ dashboard.html          # 管理后台
├─ .github/
│  ├─ scripts/
│  │  ├─ install_singbox_check.sh # 下载并校验 CI 使用的官方 sing-box 1.14.3
│  │  ├─ release_archive.py      # 发布归档创建、内容/权限和隔离解压校验
│  │  └─ test_release_archive.py # 危险归档条目和目录元数据回归测试
│  └─ workflows/
│     ├─ cd-release.yml        # push / 手动触发：Go 和归档校验测试
│     └─ release-tag.yml       # v* tag：测试、跨平台构建、校验并发布 Release
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
3. 管理后台和 API 请求进入 `controller`，通过 `lib/store.go` 读写订阅路径、节点列表和路由规则。首次升级时自动新增 `singbox_publication` 表，用于保存 sing-box 当前转换报告和上次有效配置，不改变既有节点和订阅路径。
4. 访问订阅路径时，服务读取节点列表，拼接换行文本后返回 Base64 编码结果。
5. 访问任一路由规则路径时，服务返回该规则保存的 JSON 内容。
6. 保存节点后，本地解析分享链接并生成 sing-box 完整配置。访问 `{subscription_path}/sing-box` 时返回已发布配置；已有数据库会在首次需要时自动生成，无需重新录入节点。

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

当 `settings.host` 为 `127.0.0.1`（或省略后使用该默认值）时，程序忽略整个 `settings.tls` 配置，始终以 HTTP 启动，不校验证书和私钥配置。通过 Nginx 反向代理时，由 Nginx 提供 HTTPS。其他监听地址仍按 `tls.enabled` 决定是否启用 TLS；启用时必须填写证书和私钥路径。

## 启动

```powershell
go run .
```

启动后访问：

- `/`：管理后台
- `/login`：登录页
- `{subscription_path}`：订阅地址
- `{subscription_path}/sing-box`：sing-box JSON 订阅地址
- `{route_rule_path}`：路由规则 JSON 地址
- `/api/*`：JSON API

## sing-box 订阅

原有节点文本框继续每行填写一条分享链接，两种客户端共用同一份节点数据。例如原订阅路径为 `/AbCdEf`：

| 客户端 | URL 路径 | 返回内容 |
| --- | --- | --- |
| Xray / v2rayN | `/AbCdEf` | 原有 Base64 订阅，字节生成逻辑不变 |
| sing-box | `/AbCdEf/sing-box` | 原生完整 JSON 配置 |

后台“当前服务”会展示两种链接、复制按钮及 sing-box 转换状态；通过 Nginx 访问时，前端展示和复制使用浏览器实际访问的域名、协议和外部端口。修改或随机重置订阅路径会同时使两种旧 URL 失效。JSON 路由规则不能占用派生的 sing-box 路径。

当前支持：

- VLESS：普通 TCP、TLS、Reality + Vision；保留 UUID、SNI、ALPN、显式 insecure、官方支持的 uTLS 指纹、UDP packet encoding。
- VLESS 传输：WebSocket（含 Early Data）、gRPC（普通 gun 模式）、HTTP、HTTPUpgrade，以及无附加加密的 QUIC（需要 TLS，不支持 uTLS / Reality）。
- Shadowsocks：SIP002 Base64/Base64URL userinfo、明文百分号编码 userinfo、旧式整体 Base64、IPv6、常见传统/AEAD/AEAD-2022 方法，以及受支持选项的 obfs-local、v2ray-plugin。
- VMess：v2rayN v2 Base64 JSON（数值字段兼容字符串/数字）和 `vmess://UUID@host:port?...` URL 格式；保留加密方式、alter ID、TLS 和受支持的 V2Ray 传输。JSON 的 gRPC `path` 映射为 service name，`net=h2` 映射为 TLS HTTP 传输；不兼容的 TCP HTTP 伪装不会冒充 HTTP/2。
- Trojan：`trojan://password@host[:port]`，默认 TLS / 443，支持普通 TCP 及上述受支持的 V2Ray 传输。
- Hysteria 1：`hysteria://` / `hy://`，支持 `auth`、`upmbps` / `downmbps`、UDP、xplus 混淆（`obfs=xplus&obfsParam=...`）；上下行带宽必须提供，不自动猜测。
- Hysteria 2：`hysteria2://` / `hy2://`，支持密码和 `user:password` 认证、TLS、salamander / gecko 混淆、可选带宽。Hysteria 1/2 均支持地址中的多端口或 `mport`（如 `443,5000-6000`）及 `hop-interval` / `hop_interval`；输出为 sing-box 的范围列表，不随机丢弃端口。
- TUIC v5：`tuic://UUID:password@host[:port]`，支持 `congestion-control`、`udp-relay-mode`、`udp-over-stream`、`disable-sni`、`reduce-rtt` 和 `heartbeat`；前五项兼容下划线拼写。互斥的 UDP 转发选项会报错，不自动覆盖。
- AnyTLS：`anytls://password@host[:port]`，支持 TLS / SNI / ALPN / uTLS，以及 `idle-session-check-interval`、`idle-session-timeout`、`min-idle-session`。
- HTTP(S) / SOCKS5：`http://`、`https://`、`socks://`、`socks5://`，支持 URL 编码的用户名/密码和 IPv6。默认端口分别为 80、443、1080，HTTPS 始终保留 TLS；这些地址按代理节点解析，不会由面板抓取为远程订阅。
- 名称保留中文；系统标签或重名会添加稳定后缀。参数按 URL 规则只解码一次，输入支持空行、CRLF 和 BOM；仅在 sing-box 转换中去除 BOM，不改写旧 Xray 输出。

新增协议的 TLS 默认验证服务器证书；只有链接明确指定 `insecure`、`allowInsecure`、`allow-insecure`、`allow_insecure` 或 `skip-cert-verify` 时才按其值处理，互相冲突则拒绝转换。支持 `peer` 作为 `sni` 的别名。QUIC 协议不套用 uTLS。未指定端口的 Trojan、Hysteria 1/2、TUIC 和 AnyTLS 使用 443。时间参数支持 `10s` 等 Go duration，纯整数按秒处理。

暂不支持 SSR、WireGuard、TUIC v4、SOCKS over TLS、VMess 的 Quantumult/Shadowrocket 私有格式，以及 Clash YAML / 完整客户端配置输入。XHTTP/SplitHTTP、mKCP、TCP HTTP 伪装、非标准 gRPC 模式、证书指纹固定参数（如 `pinSHA256` / `pcs`）、ECH、外部证书文件路径等也不做有损转换。未知参数、重复字段和冲突参数会显示脱敏错误码，不会忽略安全设置或悄悄降级。Reality 的 Xray spiderX 参数不映射，后台会显示警告。转换输入上限为 2048 个节点、4 MiB，每条链接最大 16 KiB，端口跳跃列表最多 128 项；这些限制不改变 Xray 的原有保存逻辑。

协议解析方式参考 [Sub-Store](https://github.com/sub-store-org/Sub-Store) 的 [URI parser](https://github.com/sub-store-org/Sub-Store/blob/master/backend/src/core/proxy-utils/parsers/index.js) 和 [sing-box producer](https://github.com/sub-store-org/Sub-Store/blob/master/backend/src/core/proxy-utils/producers/sing-box.js)。实现为本地 Go 转换器，不运行 Sub-Store / Node.js，不向第三方上传节点，也不执行分享链接携带的脚本或插件程序；支持范围以本节和测试为准，并非完整移植 Sub-Store。

### 运行模式

在后台“sing-box 订阅设置”的“运行模式”下拉框中选择并保存：

| 模式 | 配置内容 | 使用场景 |
| --- | --- | --- |
| TUN / VPN（默认） | 双栈 TUN 入站、自动路由、DNS 接管、代理 DoH、出站防环路设置；不再只开放 Mixed 端口 | Android / iOS 的 sing-box VPN 客户端，也可用于具备 TUN 权限的桌面客户端 |
| Mixed 代理端口 | 仅监听 `127.0.0.1:2080` 的 HTTP/SOCKS 代理端口，不接管系统流量 | 桌面应用手动配置代理，或交由其他程序接管流量 |

两个模式共用 `proxy` 手动选择组、`auto` 测速组、`direct` 出站，默认路由走 `proxy`；均未内置地域分流规则。Clash API 默认仅监听 `127.0.0.1:9090`，可通过 `disable_clash_api` 关闭。TUN 不固定接口名称，不启用 Linux 专用 `auto_redirect`，由手机 VPN 客户端创建虚拟网卡。

TUN 模板使用 `172.19.0.1/30` 和 `fdfe:dcba:9876::1/126` 地址、1500 MTU、`auto_route` / `strict_route`；TCP/UDP 53 端口请求交给 sing-box DNS。应用域名通过代理访问 Cloudflare DoH（`1.1.1.1`，验证 `cloudflare-dns.com` TLS 证书）；连接代理前，代理服务器域名通过直连 `223.5.5.5:53` 引导解析，避免“解析代理需要先连接代理”的循环。引导解析未加密，网络 DNS 提供方可看到代理服务器域名；普通应用的 DNS 默认不走此直连解析器。此模板不拦截应用自带的 DoH/DoT，也不能代替客户端自身的权限和泄漏防护设置。

旧配置未指定 `settings.singbox.mode` 时，升级后默认改为 TUN。需要保留原桌面端口方式时，明确选择 Mixed 或设置 `"mode": "mixed"`。后台仅修改此字段，使用同目录 `0600` 临时文件写入后替换 `config.json`，保留管理员、TLS、其他 sing-box 选项和旧兼容字段，不修改父目录权限。服务账号需有配置文件所在目录的写权限，不需要重启服务。

手机端更新步骤：

1. 后台选择“TUN / VPN（手机）”，保存后确认 sing-box 状态为“已同步”。
2. 在手机 sing-box 客户端刷新原 sing-box 远程配置，或重新导入同一个 `{subscription_path}/sing-box` 地址；不是 Xray Base64 地址。
3. 停止并重新启动该配置，允许系统 VPN 权限。桌面使用 TUN 时同样需要相应系统权限。

手机客户端版本需支持生成的配置字段。测试使用官方 sing-box 1.14.3；配置检查通过并不表示已在 Android / iOS 真机上连接成功。代理服务器、网络、客户端版本或 VPN 权限异常仍可能导致无法访问。

可在 `config.json` 的 `settings` 内增加可选的 `singbox` 配置：

```json
"singbox": {
  "mode": "tun",
  "allow_partial": false,
  "mixed_port": 2080,
  "clash_port": 9090,
  "disable_clash_api": false,
  "test_url": "https://www.gstatic.com/generate_204",
  "test_interval": "10m"
}
```

默认严格模式下，只要有节点转换失败，本次 sing-box 配置就不发布，Xray 仍正常保存。已有相同运行模式的有效配置时继续提供上次版本，后台显示“未同步”；没有有效版本时返回 HTTP 503。切换模式后不回退到另一模式的旧配置，防止手机再次收到 Mixed 模板。设置 `allow_partial: true` 可明确允许跳过不兼容节点，但至少需要一个可用节点，并在后台展示跳过数量。主动清空节点会停用 sing-box 输出，不会继续提供历史凭据。

配置按原始节点、模板版本和选项（含运行模式）的内容哈希缓存；模板升级或模式变更会触发重新转换，无需重新保存节点。节点保存和发布串行处理，完整产物保存到数据库，重启后仍可使用上次有效版本。GET 返回原始 JSON，HEAD 返回同样的状态及内容长度；两者均使用 `Cache-Control: no-store`，不对外返回转换报告。新 sing-box 订阅请求不会在 GoSub 访问日志中记录其路径 Token；Nginx 等外层代理的日志需由部署者另行管理。

字段规范与验证使用 [sing-box 1.14.3](https://github.com/SagerNet/sing-box/releases/tag/v1.14.3)、[VLESS](https://sing-box.sagernet.org/configuration/outbound/vless/)、[Shadowsocks](https://sing-box.sagernet.org/configuration/outbound/shadowsocks/)、[TLS](https://sing-box.sagernet.org/configuration/shared/tls/) 和 [V2Ray Transport](https://sing-box.sagernet.org/configuration/shared/v2ray-transport/) 官方文档。`sing-box check` 仅证明配置结构可加载，不代表实际服务器连通，也不等同于 Android/iOS 的 VPN 模板验收。

VPN 模板参照 [TUN](https://sing-box.sagernet.org/configuration/inbound/tun/)、[DNS over HTTPS](https://sing-box.sagernet.org/configuration/dns/server/https/)、[Route](https://sing-box.sagernet.org/configuration/route/) 和 [hijack-dns](https://sing-box.sagernet.org/configuration/route/rule_action/#hijack-dns) 官方文档。CI 对 TUN 和 Mixed 两套完整配置分别执行 `sing-box check`，包含以域名为服务器地址的节点。

新增协议的字段参照 [VMess](https://sing-box.sagernet.org/configuration/outbound/vmess/)、[Trojan](https://sing-box.sagernet.org/configuration/outbound/trojan/)、[Hysteria](https://sing-box.sagernet.org/configuration/outbound/hysteria/)、[Hysteria 2](https://sing-box.sagernet.org/configuration/outbound/hysteria2/)、[TUIC](https://sing-box.sagernet.org/configuration/outbound/tuic/)、[AnyTLS](https://sing-box.sagernet.org/configuration/outbound/anytls/)、[HTTP](https://sing-box.sagernet.org/configuration/outbound/http/) 和 [SOCKS](https://sing-box.sagernet.org/configuration/outbound/socks/) 官方文档，以及 [v2rayN VMess 分享格式](https://github.com/2dust/v2rayN/wiki/Description-of-VMess-share-link)、[Hysteria 2 URI](https://v2.hysteria.network/docs/developers/URI-Scheme/) 和 [AnyTLS URI](https://github.com/anytls/anytls-go/blob/main/docs/uri_scheme.md)。

## Linux 部署与更新

发布包仅包含对应平台的可执行文件、`config.example.json` 和 `README.md`。归档不包含 `./` 目录条目，也不包含实际运行的 `config.json` 或 `gosub.db`。tar 包内文件使用数字 UID/GID `0:0`，可执行文件权限为 `0755`，示例配置和文档为 `0644`，不会携带构建机器的用户或组信息。

发布前会检查文件清单、类型、属主、权限，并在 Linux 临时目录中试解压，确认目标目录的权限和所有权以及已有配置、数据库内容保持不变。tar 包还会以 root 身份在独立临时目录中重复验证，覆盖 root 默认恢复属主的场景。包含目录、链接、重复条目或额外文件的归档会阻止发布。

安装和更新时先解压到独立临时目录。以下示例使用 GNU tar，兼容包含 `./` 条目的旧版本发布包。执行前将归档路径替换为实际下载位置，并确认安装目录与现有 `gosub.service` 的 `ExecStart` 路径一致；如果服务使用 `/root/gosub/gosub`，将示例中的安装目录改为 `/root/gosub`：

```bash
archive=/absolute/path/to/gosub_linux_amd64.tar.gz
gosub_install_dir=/opt/gosub
gosub_stage=$(mktemp -d)
tar --extract --gzip --file "$archive" \
  --directory "$gosub_stage" --no-same-owner --no-overwrite-dir

sudo install -d -m 0755 "$gosub_install_dir"
sudo systemctl stop gosub
sudo install -m 0755 "$gosub_stage/gosub_linux_amd64" "$gosub_install_dir/gosub"
sudo systemctl start gosub
```

更新时保留原有 `WorkingDirectory`、配置和数据库位置；首次安装还需要先配置服务。不要用 `config.example.json` 覆盖已有的 `config.json`。

`--no-same-owner` 避免恢复归档中的属主，`--no-overwrite-dir` 保留已有目录的权限和所有权。不要直接在 `/root` 或 `/` 中解压旧包，也不要通过递归 `chown` 或 `chmod` 修复部署目录。[GNU tar 参数说明](https://www.gnu.org/software/tar/manual/html_node/Option-Summary.html)

此修复只影响重新构建和发布的归档，已下载的旧版本包需要继续使用以上解压参数。

## API

除 `POST /api/login` 外，所有管理 API 都需要 `session_id` Cookie。

- `POST /api/login`：表单参数 `username`、`password`
- `GET /api/session`
- `POST /api/logout`
- `GET /api/config`
- `POST /api/change_uri`：表单参数 `uri`
- `POST /api/change_nodes`：可重复表单参数 `nodes`
- `GET {subscription_path}`：返回 Base64 订阅文本
- `GET/HEAD {subscription_path}/sing-box`：返回完整 sing-box JSON / 响应元数据
- `POST /singbox-settings`：登录后以表单 `mode=tun` 或 `mode=mixed` 保存运行模式；受同源写请求保护，返回更新后的后台页面或 JSON（`Accept: application/json`），包含 `singbox_mode` 和转换报告
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

指定官方 sing-box 程序后，同一组测试还会分别校验包含各类受支持节点的完整 TUN / Mixed 配置（程序仅执行 `check`，不会启动代理或 VPN 服务）：

```powershell
$env:SINGBOX_BINARY = "C:\path\to\sing-box.exe"
go test -count=1 ./...
```

CI 固定使用 sing-box 1.14.3，下载后校验官方 SHA256 再运行以上配置检查。未指定程序且 PATH 中没有 `sing-box` 时，该项会显式跳过。

归档校验测试（仅使用 Python 3 标准库）：

```bash
python3 -m unittest discover -s .github/scripts -p 'test_*.py' -v
```

CI 会在 push 和 release tag 流程中运行以上测试，并通过 `release_archive.py` 创建和校验各平台的发布包。打包和校验只使用 Python 3 标准库，Linux 试解压使用 GNU tar。SQLite driver 使用纯 Go 实现，便于 `CGO_ENABLED=0` 跨平台构建。
