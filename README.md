# gosub

gosub 是一个本地代理订阅管理服务。当前技术栈为 Gin + GORM + Go 标准库 `html/template` + 原生 JavaScript；运行配置保存在 `config.json`，订阅路径、节点列表、路由规则和 sing-box 模板保存在 SQLite。

## 功能

- 订阅分发：访问保存的订阅路径时，返回 Base64 编码后的节点列表。
- sing-box 订阅：支持 VLESS、Shadowsocks、VMess、Trojan、Hysteria 1/2、TUIC v5、AnyTLS、HTTP(S) 和 SOCKS5 分享链接，本地生成独立的完整 sing-box JSON 配置，原 Xray URL 和 Base64 输出保持不变。
- sing-box 模板：SQLite 保存完整 JSON 模板，控制 DNS、路由、入站、选择组等配置，生成订阅时只插入转换后的节点；内置 TUN / VPN 和 Mixed 预设作为修改起点。
- 管理后台：订阅概览、配置管理、路由规则分区展示；节点、路径和模板在独立弹窗中编辑，保存后显示结果提示。
- 路由规则：下拉选择、默认折叠编辑，展示全部规则的名称和订阅路径，支持新增、修改、删除。
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
│  └─ security_test.go        # 代理头、Cookie、限速、请求限制、会话撤销、日志和 XSS 回归
├─ model/
│  └─ model.go                # GORM 模型：节点、路径、规则、sing-box 模板和发布缓存
├─ lib/
│  ├─ config.go               # 配置加载/校验、私有文件原子保存、鉴权/路径校验
│  ├─ config_test.go          # 本地监听忽略 TLS 和其他监听地址的配置校验测试
│  ├─ store.go                # SQLite/GORM 初始化、迁移、节点/订阅/路由规则 CRUD
│  ├─ session.go              # 内存 session 存储和 session token 生成
│  ├─ random.go               # 加密随机字符串、公开路径、用户 ID 生成
│  ├─ b64enc.go               # 节点拼接和 Base64 订阅文本生成
│  ├─ singbox.go              # 分享链接解析、协议参数转换和 sing-box 配置组装
│  ├─ singbox_test.go         # 转换、缓存/并发、官方 check 和 DNS 启动测试
│  ├─ singbox_template.go     # 内置预设、模板校验、SQLite 保存、节点和选择组注入
│  ├─ singbox_template_test.go # 旧库迁移、模板保真、非法配置、持久化和缓存隔离测试
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
6. 首次启动自动迁移 SQLite 表结构，在 `singbox_template` 表中初始化模板，已有模板不覆盖。保存节点或模板后，本地解析分享链接，将节点插入模板并生成 sing-box 配置。访问 `{subscription_path}/sing-box` 时返回发布结果；模板不另存为运行 JSON 文件。

## 配置

首次启动如果没有 `config.json`，程序会生成默认配置并继续启动。默认监听 `127.0.0.1:8080`，管理员账号和 32 位随机密码独立生成，请在本机查看 `config.json`；启动日志不输出密码或订阅令牌。

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

`settings.trusted_proxies` 可配置可信代理 IP/CIDR。省略或为 `null` 时仅信任 `127.0.0.1/32` 和 `::1/128`，空数组 `[]` 禁用代理头信任。跨主机或容器代理需显式填写其实际来源地址，不要配置 `0.0.0.0/0`、`::/0` 或不可信网段；变更后重启服务。非可信来源的转发头不会影响真实 IP、HTTPS 判断、Cookie 或登录限速。

Nginx 应覆盖转发头，保留外部端口。非标准 HTTPS 端口不要用会丢失端口的 `$host` 替代 `$http_host`：

```nginx
location / {
    proxy_pass http://127.0.0.1:12345;
    proxy_set_header Host $http_host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 16m;
}
```

应用会校验写请求的协议、主机和有效端口；若反代外部 HTTPS/端口信息不一致会返回 403，不建议通过关闭同源保护解决。浏览器在 `no-referrer` 策略下提交表单可能发送 `Origin: null`；仅当 `Sec-Fetch-Site: same-origin` 明确证明同源时接受此类请求，不会一律放行空来源或回环地址。

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

后台“订阅概览”展示两种链接、复制按钮及 sing-box 转换状态；通过 Nginx 访问时，前端展示和复制使用浏览器实际访问的域名、协议和外部端口。修改或随机重置订阅路径会同时使两种旧 URL 失效。JSON 路由规则不能占用派生的 sing-box 路径。

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

### 数据库模板与预设

在后台“配置管理”点击“编辑模板”，可粘贴完整 JSON、载入预设、修改并保存。编辑器位于弹窗中，不占用主页；保存失败会保留数据库中的原模板。首次打开即显示已保存模板，而不是空白填写示例。

模板仅由程序内置默认值和 SQLite 运行数据管理，不提供独立模板文件：

- 内置预设：默认值定义在 [`lib/singbox_template.go`](lib/singbox_template.go)，随程序编译，用于初始化 SQLite 模板及网页“载入预设”。无需部署、读取或维护额外 JSON 文件。
- 运行模板：保存在 `gosub.db` 的 `singbox_template` 表中。仅在无模板记录时根据 `settings.singbox` 选项和内置预设初始化；后续启动、升级和预设更新不覆盖已保存模板。不读取、导入或生成独立的运行模板 JSON 文件。

网页提供两种本地预设，载入后可修改，点击“保存模板”才写入 SQLite 并更新订阅：

| 模式 | 配置内容 | 使用场景 |
| --- | --- | --- |
| TUN / VPN（默认） | 双栈 TUN 入站、自动路由、DNS 接管、代理 DoH、出站防环路设置；不再只开放 Mixed 端口 | Android / iOS 的 sing-box VPN 客户端，也可用于具备 TUN 权限的桌面客户端 |
| Mixed 代理端口 | 仅监听 `127.0.0.1:2080` 的 HTTP/SOCKS 代理端口，不接管系统流量 | 桌面应用手动配置代理，或交由其他程序接管流量 |

两个预设共用 `proxy` 手动选择组、`auto` 测速组、`direct` 出站，默认路由走 `proxy`；均未内置地域分流规则。Clash API 默认仅监听 `127.0.0.1:9090`；可在模板中修改或删除 `experimental.clash_api`。TUN 不固定接口名称，不启用 Linux 专用 `auto_redirect`，由手机 VPN 客户端创建虚拟网卡。

参照 [sing-box 官方配置结构](https://sing-box.sagernet.org/configuration/)，模板可包含 `$schema`、`log`、`dns`、`ntp`、`certificate`、`certificate_providers`、`http_clients`、`network_namespaces`、`endpoints`、`inbounds`、`outbounds`、`route`、`services`、`experimental` 等字段。除自动插入的节点和选择组成员展开外，其他配置原样保留，包括自定义路由、规则集、DNS、TUN、控制器和未来新增字段；JSON 排版和键顺序可能变化，不删除未知字段。

自动生成的节点会追加到顶层 `outbounds`。在 `selector` / `urltest` 的 `outbounds` 成员数组中使用字符串 `"{{nodes}}"`，会展开为所有成功转换的节点标签。例如：

```json
{
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["auto", "direct", "{{nodes}}"], "default": "auto"},
    {"type": "urltest", "tag": "auto", "outbounds": ["{{nodes}}"], "url": "https://www.gstatic.com/generate_204", "interval": "10m"},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"final": "proxy"}
}
```

这个片段仅演示节点注入，不是手机 VPN 完整模板；手机请基于本地 TUN 预设修改。选择组成员缺失或为空数组时也会自动加入所有节点；显式填写其他成员时保留其顺序并去重。模板内静态出站和端点保留，自动节点与其标签冲突时添加稳定后缀。占位符只在上述组成员数组中展开，不用于替换任意文本，也不支持按地区筛选。

模板必须是标准 JSON 对象，不支持注释或尾随逗号，最多 512 KiB、64 层嵌套。保存时检查重复 JSON 字段、出站标签、组成员引用、选择组循环、`default`、`route.final` 及已知的空 direct DNS detour 问题。校验不是完整的 sing-box 配置 schema 检查；自定义字段和客户端版本兼容性应通过对应版本的 `sing-box check -c <生成配置.json>` 验证。预设包含占位符，应检查最终生成的订阅 JSON，而不是编辑器中的模板草稿。

TUN 模板使用 `172.19.0.1/30` 和 `fdfe:dcba:9876::1/126` 地址、1500 MTU、`auto_route` / `strict_route`；TCP/UDP 53 端口请求交给 sing-box DNS。应用域名通过代理访问 Cloudflare DoH（`1.1.1.1`，验证 `cloudflare-dns.com` TLS 证书）；连接代理前，代理服务器域名通过直连 `223.5.5.5:53` 引导解析，避免“解析代理需要先连接代理”的循环。引导解析未加密，网络 DNS 提供方可看到代理服务器域名；普通应用的 DNS 默认不走此直连解析器。此模板不拦截应用自带的 DoH/DoT，也不能代替客户端自身的权限和泄漏防护设置。

`dns-bootstrap` 使用新版 UDP DNS 自带的直连拨号器，必须省略 `detour`，而不是填写 `"detour": "direct"`：指向无附加设置的 direct 出站会在 DNS 启动阶段报 `detour to an empty direct outbound makes no sense`。`dns-remote` 的 `"detour": "proxy"` 则保留。字段行为见 [UDP DNS 官方说明](https://sing-box.sagernet.org/configuration/dns/server/udp/)。修正后的模板版本会使旧缓存自动重新生成；部署新版 GoSub 后，手机端刷新订阅并重新启动配置即可，不需重新录入节点。

网页保存使用原子 SQLite upsert，不改写 `config.json`、管理员凭据或父目录权限。服务账号需有数据库及其目录的写权限，无需重启；无需在服务器手工编辑 JSON 文件。模板不可用时可在网页载入预设修复；保存前请备份需要保留的自定义内容。

手机端更新步骤：

1. 后台“编辑模板”中选择“TUN / VPN”，点击“载入预设”，按需修改后保存，确认 sing-box 状态为“已同步”。
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

`allow_partial` 继续控制转换失败策略；其他字段只用于首次初始化数据库模板和旧版预设接口。模板一旦存在，DNS、端口、测速、Clash API 等以数据库模板为准，修改旧配置选项不会覆盖模板。网页“载入预设”使用内置标准默认值，不沿用当前自定义模板的端口或密钥。

默认严格模式下，只要有节点转换失败，本次 sing-box 配置就不发布，Xray 仍正常保存。仅在模板内容相同、且上次发布的源节点仍全部保留时提供上次版本，后台显示“未同步”；否则撤销旧输出并返回 HTTP 503。删除、替换节点凭据或修改模板后，即使新转换失败，也不会继续下发被撤销的旧节点、DNS、路由或入站设置。设置 `allow_partial: true` 可明确允许跳过不兼容节点，但至少需要一个可用节点，并在后台展示跳过数量。主动清空节点会停用 sing-box 输出，不会继续提供历史凭据。

配置按原始节点、数据库模板内容、转换器版本和 `allow_partial` 哈希缓存。节点保存、模板保存和发布串行处理，完整产物及源节点指纹保存到数据库，重启后仍按同样的凭据撤销规则处理。损坏模板会停止对应输出，不会继续返回其他模板的历史配置；管理后台与 Xray 不受该转换错误影响。GET 返回原始 JSON，HEAD 返回同样的状态及内容长度；两者均使用 `Cache-Control: no-store`，不对外返回转换报告。GoSub 日志不记录订阅路径 Token、查询串或请求头；Nginx 等外层代理的日志需由部署者另行管理。

字段规范与验证使用 [sing-box 1.14.3](https://github.com/SagerNet/sing-box/releases/tag/v1.14.3)、[VLESS](https://sing-box.sagernet.org/configuration/outbound/vless/)、[Shadowsocks](https://sing-box.sagernet.org/configuration/outbound/shadowsocks/)、[TLS](https://sing-box.sagernet.org/configuration/shared/tls/) 和 [V2Ray Transport](https://sing-box.sagernet.org/configuration/shared/v2ray-transport/) 官方文档。`sing-box check` 仅证明配置结构可加载，不代表实际服务器连通，也不等同于 Android/iOS 的 VPN 模板验收。

VPN 模板参照 [TUN](https://sing-box.sagernet.org/configuration/inbound/tun/)、[DNS over HTTPS](https://sing-box.sagernet.org/configuration/dns/server/https/)、[Route](https://sing-box.sagernet.org/configuration/route/) 和 [hijack-dns](https://sing-box.sagernet.org/configuration/route/rule_action/#hijack-dns) 官方文档。CI 对 TUN 和 Mixed 两套完整配置分别执行 `sing-box check`，包含以域名为服务器地址的节点。另以无入站、无测速的隔离配置实际启动相同 DNS 拓扑，断言旧 detour 能复现启动错误、修正后的配置能完成初始化；检查到启动成功后立即终止并等待进程退出。该测试不创建 TUN、不监听代理/控制端口，也不验证节点或真实 DNS 连通性。

新增协议的字段参照 [VMess](https://sing-box.sagernet.org/configuration/outbound/vmess/)、[Trojan](https://sing-box.sagernet.org/configuration/outbound/trojan/)、[Hysteria](https://sing-box.sagernet.org/configuration/outbound/hysteria/)、[Hysteria 2](https://sing-box.sagernet.org/configuration/outbound/hysteria2/)、[TUIC](https://sing-box.sagernet.org/configuration/outbound/tuic/)、[AnyTLS](https://sing-box.sagernet.org/configuration/outbound/anytls/)、[HTTP](https://sing-box.sagernet.org/configuration/outbound/http/) 和 [SOCKS](https://sing-box.sagernet.org/configuration/outbound/socks/) 官方文档，以及 [v2rayN VMess 分享格式](https://github.com/2dust/v2rayN/wiki/Description-of-VMess-share-link)、[Hysteria 2 URI](https://v2.hysteria.network/docs/developers/URI-Scheme/) 和 [AnyTLS URI](https://github.com/anytls/anytls-go/blob/main/docs/uri_scheme.md)。

## Linux 部署与更新

发布包仅包含对应平台的可执行文件、`config.example.json` 和 `README.md`，不附带 sing-box 模板文件。归档不包含 `./` 目录条目，也不包含实际运行的 `config.json` 或 `gosub.db`。tar 包内文件使用数字 UID/GID `0:0`，可执行文件权限为 `0755`，示例配置和文档为 `0644`，不会携带构建机器的用户或组信息。

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

更新时保留原有 `WorkingDirectory`、配置和数据库位置；首次安装还需要先配置服务。不要用示例覆盖已有 `config.json`，不要删除旧数据库。sing-box 模板直接在网页编辑并保存到 SQLite，已有模板不会被升级覆盖。

`--no-same-owner` 避免恢复归档中的属主，`--no-overwrite-dir` 保留已有目录的权限和所有权。不要直接在 `/root` 或 `/` 中解压旧包，也不要通过递归 `chown` 或 `chmod` 修复部署目录。[GNU tar 参数说明](https://www.gnu.org/software/tar/manual/html_node/Option-Summary.html)

此修复只影响重新构建和发布的归档，已下载的旧版本包需要继续使用以上解压参数。

### 旧数据库升级

停服后先备份 `config.json` 和完整 SQLite 数据，再替换二进制并启动。离线备份时如存在 `gosub.db-wal` / `gosub.db-shm` 等伴随文件，也需保持一致；不要在运行中只复制主库。备份含节点和密钥，应限制访问权限。

新版启动时通过 GORM `AutoMigrate` 自动增加缺失的表和字段，例如 `singbox_template` 表和发布缓存的源节点指纹字段；不会清空节点、订阅路径或路由规则。没有模板记录时初始化内置预设，有记录时保留已有模板。迁移可重复运行，无需手工执行建表 SQL，也无需单独导入模板文件。测试覆盖有订阅表的旧库、历史 `settings.path` 数据以及重复启动；未知版本、损坏数据库或写入权限异常仍可能导致迁移失败，应保留备份以便恢复。

## API

除 `POST /api/login` 外，所有管理 API 都需要 `session_id` Cookie。

- `POST /api/login`：表单参数 `username`、`password`
- `GET /api/session`
- `POST /api/logout`
- `GET /api/config`：管理员密码字段返回空字符串，不回传密码
- `POST /api/change_uri`：表单参数 `uri`
- `POST /api/change_nodes`：可重复表单参数 `nodes`
- `GET {subscription_path}`：返回 Base64 订阅文本
- `GET/HEAD {subscription_path}/sing-box`：返回完整 sing-box JSON / 响应元数据
- `GET /api/singbox-template`：登录后获取当前模板文本、`storage=sqlite`、表名、TUN / Mixed 预设和可选的 `read_error`；模板不会包含在普通后台 JSON 中
- `POST /singbox-template`：登录后以表单参数 `template` 保存完整 JSON 模板，受同源写请求保护，返回后台页面或 JSON（`Accept: application/json`）和转换报告
- `POST /singbox-settings`：保留的旧版接口，`mode=tun` / `mode=mixed` 会更新旧配置选项并将运行模板替换为对应预设；会覆盖自定义模板，新界面不调用此接口
- `GET {route_rule_path}`：返回对应路由规则 JSON

管理后台表单接口同样使用 `session_id` Cookie：

- `POST /subscription-path`：保存或随机生成订阅路径
- `POST /nodes`：保存节点列表
- `POST /route-rules`：新增路由规则，表单参数 `name`、`path`、`content`、`submit_action`
- `POST /route-rules/{id}`：修改路由规则
- `POST /route-rules/{id}/delete`：删除路由规则

## 安全性

2026-10-20 的源码、依赖与发布流程审计及修补记录见 [安全审计报告](SECURITY_AUDIT.md)。以下保护不等于对操作系统、部署网络或客户端的绝对安全保证。

- 管理接口使用 `session_id` Cookie 鉴权，Cookie 设置为 `HttpOnly` 和 `SameSite=Lax`；原生 HTTPS 或可信代理转发 HTTPS 时设置 `Secure`。会话有效期 12 小时，重新登录会替换当前会话，修改管理员或密码后旧会话失效。
- 登录页面和 API 共用限速：每 IP 每 5 分钟最多 10 次，服务全局最多 300 次；触发时返回 429 和 `Retry-After`。
- 后台写请求检查 `Origin` / `Referer` 的协议、主机和端口，并检查 Fetch Metadata，跨源写入会被拒绝。无浏览器来源头的直接 API 调用仍需有效会话。
- 写接口只接受 URL 编码表单。登录请求最多 8 KiB，节点接口 16 MiB，其余写接口 2 MiB；拒绝错误编码和重复字段（API 的重复 `nodes` 除外）。受保护写请求先鉴权再解析正文，同时最多处理 4 个写请求。
- HTTP 配置请求头 5 秒、请求读写各 30 秒、空闲 60 秒超时及 16 KiB 请求头限制；TLS 同端口分流最多同时分类 128 个连接，最低 TLS 1.2。
- 登录账号和密码比较使用 SHA-256 后的固定时间比较，减少时序泄露。
- `config.json` 和 `gosub.db` 尽量设置为 `0600` 权限，数据库在迁移前按私有权限创建；不修改父目录权限，Windows 仍需用 ACL 控制访问。SQLite 启用 `secure_delete`，减少删除凭据在空闲页中的残留。
- 新生成的订阅路径和路由规则路径使用加密随机数，默认 16 位字母随机片段。
- 公开路径会拒绝空值、空白字符、查询串、fragment、过长路径和系统保留路径，例如 `/api`、`/assets`、`/login`、`/route-rules`。
- 节点保存限制 2048 条、合计 4 MiB、单条 16 KiB；超限拒绝保存，不影响原数据。路由规则和模板 JSON 限制为 512 KiB、64 层，拒绝重复字段，格式化后的存储体积也受限。
- 响应会设置 CSP、`X-Frame-Options`、`X-Content-Type-Options`、`Referrer-Policy`，非静态响应默认 `Cache-Control: no-store`。
- 原生 HTTPS 或可信代理 HTTPS 返回 HSTS。访问日志仅记录注册路由模式，异常处理不记录请求、Cookie、密码或订阅路径。
- CI 和发布任务固定 Action 提交 SHA，不持久化 checkout 凭据，执行 `go vet`、竞态测试和官方 `govulncheck`；归档拒绝危险目录、链接、属主、权限和额外 PAX 元数据。

需要注意的边界：

- 订阅路径和路由规则路径是公开读取 URL，设计上不要求登录；安全性依赖随机路径的不可猜测性。不要使用容易猜到的路径。
- `config.json` 中的管理员密码仍是明文配置项，应只放在可信机器上，并限制文件访问权限。
- 如果把 `host` 改成 `0.0.0.0` 对外监听，请启用 TLS、使用高强度密码，并通过防火墙或反向代理限制访问面。
- 内存 session 会在程序重启后失效；这有利于减少持久化风险，但重启后需要重新登录。
- 模板可包含控制器密钥、静态出站密码等敏感信息，仅管理员接口可以编辑读取，不作为静态资源公开；生成后的相关字段会随订阅下发，因此订阅 URL 也需保密。模板与节点同库存储，数据库备份也应按密钥管理。
- 旧版默认密码若与账号共享随机后缀，或密码、订阅路径曾进入日志，应主动更换管理员密码及随机订阅路径；补丁不会擅自修改现有凭据，也不能撤回客户端已下载的数据或清除历史备份。
- 模板中的远程规则集 URL、本地证书路径等只交给客户端使用，GoSub 不执行模板、不读取这些路径、不抓取这些 URL。请勿粘贴不可信模板，尤其注意远程规则集、监听地址、DNS、路由和证书验证设置。

## 构建

要求 Go 1.27.3 或更新的已修复版本，`go.mod` 已提高最低版本。开启自动工具链下载时 Go 会按需获取；离线构建需预先安装。标准库安全修复随二进制构建版本生效，仅替换源码不会修复已部署的旧二进制。参见 [Go 发布与安全修复记录](https://go.dev/doc/devel/release)。

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

测试覆盖旧 SQLite 自动迁移、模板字段保留、节点占位符展开、标签冲突、无效 JSON/重复字段/循环引用、数据库保存失败与重启、并发发布、模板及节点变更后的缓存撤销，以及代理信任、登录限速、会话/同源保护和日志脱敏。

安全检查：

```bash
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go test -race ./...
```

竞态测试需要 CGO 和 C 编译器；没有该工具链的 Windows 环境不能执行，CI 的 Linux runner 会运行。漏洞扫描依赖官方在线数据库，只反映扫描时已公开且可达的漏洞，不能取代源码审查。

指定官方 sing-box 程序后，同一组测试还会分别校验包含各类受支持节点的完整 TUN / Mixed 配置，并运行上述 DNS 启动回归测试（不会创建 VPN 或监听端口，测试进程自动关闭）：

```powershell
$env:SINGBOX_BINARY = "C:\path\to\sing-box.exe"
go test -count=1 ./...
```

CI 固定使用 sing-box 1.14.3，下载后校验官方 SHA256 再运行以上配置检查和 DNS 启动测试。未指定程序且 PATH 中没有 `sing-box` 时，这两项会显式跳过。

归档校验测试（仅使用 Python 3 标准库）：

```bash
python3 -m unittest discover -s .github/scripts -p 'test_*.py' -v
```

CI 会在 push 和 release tag 流程中运行以上测试，并通过 `release_archive.py` 创建和校验各平台的发布包。打包和校验只使用 Python 3 标准库，Linux 试解压使用 GNU tar。SQLite driver 使用纯 Go 实现，便于 `CGO_ENABLED=0` 跨平台构建。
