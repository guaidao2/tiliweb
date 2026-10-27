# tiliweb

**tiliweb** 是一个面向国内环境、开源自托管的 Go CMS 框架。单二进制、SQLite 默认存储、服务端渲染模板，前后台账号完全分离，内置论坛与 MCP 接口，方便 AI 辅助运维。

> 作者：**guaidao2**

## 特性

- **单二进制部署**：模板和静态资源 `embed` 进二进制，不依赖 Node.js；可选磁盘模板和 `--dev` 热重载。
- **SQLite 默认**：纯 Go 驱动 `modernc.org/sqlite`，内置前向迁移，适合单机、VPS 和小团队站点。
- **前后台完全分离**：`admin_users` / `users`、两套会话、两套登录入口；后台只用密码，保留扩展点。
- **内容管理**：栏目、条目、独立页面、标签、Markdown、修订历史、软删除/回收站、发布/草稿。
- **官网展示**：首页 Banner/精选/最新/产品/案例/CTA、栏目列表、条目详情、标签页、关于页。
- **官网扩展**：招聘/产品/案例/下载扩展字段、附件强制下载、友情链接、301/302 站内重定向、评论审核。
- **联系与留言**：固定 `/contact`，匿名留言带双提交 CSRF、蜜罐、最短填写时间和限流；后台可标记状态、写备注。
- **中文搜索**：SQLite FTS5 + CJK 逐字归一化，「产品」「案例」等两字词可命中；FTS 不可用时降级 `LIKE`。
- **论坛**：两级板块、主题/回复/楼层、置顶/锁定/删除/恢复、版主任命、新用户审核、论坛搜索；游客可读公开板块。
- **SEO**：每页 TDK、canonical、Open Graph、Twitter Card、JSON-LD、RSS、sitemap、robots。
- **媒体与上传**：图片 MIME/魔数/大小/像素校验，原图 + 320/768/1280 缩略图，路径穿越防护；附件类型/文件头白名单。
- **MCP / AI 运维**：stdio + 独立 HTTP 监听，Bearer Token、作用域、写权限开关、调用审计、限流；默认只读。
- **运维**：可配置监听地址/端口、base path、反代/WAF 适配、健康检查、优雅关闭、日志轮转、SQLite/媒体备份。
- **硬化**：CSP/HSTS/COOP 等安全头、登录/留言/评论/发帖限流、上传白名单、CSRF、软删除和 CLI 备份。

## 快速开始

环境要求：Go 1.27+。

```bash
cp config.example.yaml config.yaml
go run ./cmd/tiliweb migrate --config config.yaml
go run ./cmd/tiliweb admin create --config config.yaml --username admin --password '你的强密码'
go run ./cmd/tiliweb serve --config config.yaml
```

打开 <http://127.0.0.1:8080/>，后台默认在 <http://127.0.0.1:8080/admin>。

健康检查：

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/readyz
```

## 监听地址与端口

tiliweb 不绑定固定端口，方便放反向代理或 WAF 后面：

```yaml
server:
  listen: "127.0.0.1:8080"   # 只监听本机，交给 nginx/Caddy
  # listen: "0.0.0.0:8080"  # 直接对外监听
  base_url: "https://www.example.com"
  base_path: ""             # 子路径部署时例如 "/cms"
```

也可以用命令行覆盖：

```bash
go run ./cmd/tiliweb serve --config config.yaml --listen 0.0.0.0:8080
go run ./cmd/tiliweb serve --config config.yaml --host 127.0.0.1 --port 8088
```

`base_url` 决定 canonical、RSS/sitemap 的绝对链接和 Cookie `Secure` 行为；上 HTTPS 后请同步设置 `cookie_secure: true`。

## 构建

```bash
go build -o tiliweb ./cmd/tiliweb
```

交叉编译：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tiliweb-linux-amd64 ./cmd/tiliweb
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o tiliweb-windows-amd64.exe ./cmd/tiliweb
```

## 配置

- 优先级：命令行 flags > `TILIWEB_*` 环境变量 > YAML > 内置默认值。
- 检查配置但不启动：

```bash
go run ./cmd/tiliweb config check --config config.yaml
```

- 开发模式（磁盘模板 + 热重载）：

```bash
go run ./cmd/tiliweb serve --dev --config config.yaml
```

常用配置项：

- `server.listen` / `server.base_url` / `server.base_path`
- `database.dsn` / `database.busy_timeout`
- `admin.path` / `admin.session_ttl` / `admin.session_idle`
- `auth.register_enabled` / `auth.register_mode`
- `site.name` / `site.contact` / `site.seo`
- `rate_limit.contact_per_hour` / `rate_limit.post_per_minute`
- `uploads.max_image_bytes` / `uploads.image_max_pixels` / `uploads.max_document_bytes`
- `mcp.enabled` / `mcp.allow_write` / `mcp.allow_admin_scope`

完整示例见 [`config.example.yaml`](config.example.yaml)。

## 备份

```bash
# SQLite 一致性快照（默认写入 storage.backup_dir）
go run ./cmd/tiliweb backup --config config.yaml

# 数据库 + 媒体/附件 zip
go run ./cmd/tiliweb backup --config config.yaml --with-media
```

建议 cron 每日执行并复制到另一台机器。硬删除操作保留在 CLI，不暴露给 MCP。

## MCP（AI 运维）

MCP 支持 stdio 与独立 HTTP 监听，共用同一套工具、作用域和审计。HTTP MCP 不在主站端口上，默认监听 `127.0.0.1:8090`，入口固定为 `/mcp`。

### 1. 开启 HTTP MCP

`config.yaml` 示例：

```yaml
server:
  # 走 nginx/Caddy 反代时，把代理地址加入可信列表
  trusted_proxies: ["127.0.0.1"]

mcp:
  enabled: true
  allow_write: false       # 需要写工具时再改 true
  allow_admin_scope: false # 需要 admin 作用域时再改 true
  http:
    enabled: true
    listen: "127.0.0.1:8090"
    public: false          # 走 TLS 反代时设为 true
    tls:
      enabled: false       # 非 loopback 直连必须开启并配置证书
      cert_file: ""
      key_file: ""
    allowed_ips: []        # 可选：限制来源 IP/CIDR
```

```bash
# serve 会同时启动主站和 MCP HTTP 监听
go run ./cmd/tiliweb serve --config config.yaml
```

看到 MCP 监听启动后，HTTP 入口就是：

```text
http://127.0.0.1:8090/mcp
```

公网部署不要把 `8090` 直接暴露。建议只监听 loopback，由 nginx/Caddy 终止 TLS，使用 `deploy/nginx-mcp.conf` 示例；反代时设置 `mcp.http.public: true`，并把 `127.0.0.1` 加入 `server.trusted_proxies`，否则 `allowed_ips` 看到的会是代理地址。直接绑定非 loopback 地址时，`mcp.http.tls.enabled` 必须为 `true`。

### 2. 创建令牌

```bash
# 只读令牌
go run ./cmd/tiliweb mcp token create --config config.yaml \
  --name ai-ops --scopes read --ttl 720h

# 需要写内容时
go run ./cmd/tiliweb mcp token create --config config.yaml \
  --name ai-writer --scopes read,content.write --ttl 168h
```

可用作用域：

```text
read               只读查询
content.write      内容创建/更新/发布/软删除
comment.moderate   评论审核
forum.moderate     论坛版务
user.moderate      前台用户状态
maintenance        重建搜索索引、清理会话
backup             查看备份
admin              管理员等价（需同时开启 allow_write 和 allow_admin_scope）
```

令牌只在创建时显示一次，例如：

```text
token: tili_mcp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
id: 1 prefix: tili_mcp_xxxx scopes: read
```

```bash
# 查看/吊销
go run ./cmd/tiliweb mcp token list --config config.yaml
go run ./cmd/tiliweb mcp token revoke --config config.yaml --id 1
```

### 3. 用 curl 操控 HTTP MCP

把创建时输出的 token 放进环境变量：

```bash
export TILIWEB_MCP_TOKEN='tili_mcp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'
```

先列工具：

```bash
curl -sS http://127.0.0.1:8090/mcp \
  -H "Authorization: Bearer $TILIWEB_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

调用只读工具：

```bash
curl -sS http://127.0.0.1:8090/mcp \
  -H "Authorization: Bearer $TILIWEB_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tiliweb.site.status","arguments":{}}}'
```

调用写工具（令牌需要 `mcp.allow_write: true` 和对应作用域）：

```bash
curl -sS http://127.0.0.1:8090/mcp \
  -H "Authorization: Bearer $TILIWEB_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "tiliweb.content.entries.create",
      "arguments": {
        "channel_id": 1,
        "title": "示例文章",
        "body_md": "由 HTTP MCP 创建。",
        "status": "draft"
      }
    }
  }'
```

返回是标准 JSON-RPC；`result.content[0].text` 里是工具结果的 JSON 字符串。无 Token 或 Token 无效返回 `401`；Token 没有对应作用域时，工具调用返回 `isError: true` 的 JSON-RPC 结果。

HTTP transport 只接受 `POST`：`GET` 不会开启 SSE，返回 `405`；`DELETE` 返回 `204`。`initialize`、`ping`、`tools/list`、`tools/call` 都可以直接 POST：

```json
{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"curl","version":"1.0"}}}
```

### 4. 接入支持 HTTP 的 AI 客户端

不同客户端的字段名可能不同，但本质都是“URL + Authorization 头”。常见形式：

```json
{
  "mcpServers": {
    "tiliweb": {
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer tili_mcp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

如果客户端只支持 stdio，则改用本地进程：

```json
{
  "mcpServers": {
    "tiliweb": {
      "command": "/opt/tiliweb/tiliweb",
      "args": ["mcp", "--config", "/etc/tiliweb/config.yaml"]
    }
  }
}
```

### 5. 安全边界

- 默认只读；写工具需要 `mcp.allow_write=true` 和对应作用域；`admin` 令牌还需要 `mcp.allow_admin_scope=true`。
- 公网暴露必须 TLS；非 loopback 未开 TLS 会拒绝启动。建议只监听本机由反代转发。
- 每次调用写 `mcp_tool_calls` 与审计；不提供任意 SQL、Shell、任意文件读写或模板编辑。
- 硬删除只保留在 CLI；MCP 只做软删除/恢复。
- 令牌只在创建时显示一次，支持 list/revoke、过期、每令牌/全局限流；建议短 TTL，按用途拆分令牌。
- WAF 需要放行 `Authorization` 和 `application/json`，并且不要缓存 `/mcp` 响应。

## 目录结构

```text
cmd/tiliweb           CLI（serve/migrate/admin/config/mcp/backup/version）
internal/app          启动装配、日志轮转、优雅关闭
internal/config       配置加载、校验、环境变量覆盖
internal/httpserver   路由、中间件、前后台处理器
internal/content      内容、媒体、评论、附件、FTS
internal/site         站点设置、联系留言
internal/forum        板块、主题、回复、版主、论坛搜索
internal/mcp          MCP 令牌、工具、stdio/HTTP、审计
internal/backup       SQLite 快照与媒体 zip
internal/store        SQLite、迁移
internal/view         模板加载（embed/磁盘/热重载）
web                   内嵌模板与静态资源
deploy                systemd/nginx 示例（不使用 Docker）
```

## 反向代理与 WAF

- 默认只监听 `127.0.0.1`，由 nginx/Caddy 终止 TLS。
- 反向代理需要传递 `Host`、`X-Forwarded-For`、`X-Forwarded-Proto`，并把 `server.trusted_proxies` 设为代理地址。
- WAF 需要放行后台路径、`/contact` POST、评论/论坛 POST、`/mcp` 的 `Authorization` 和 JSON 内容类型；不要缓存后台和 MCP 响应。
- 子路径部署时设置 `server.base_path`，并让反代去掉该前缀后再转发。

## 开发与测试

```bash
go vet ./...
go test ./...
go build ./...
```

需要 `-race` 时确保本机有 CGO 和 C 编译器：

```bash
CGO_ENABLED=1 go test -race ./...
```

## 作者与许可

- 作者：**guaidao2**
- 许可证：MIT，见 [LICENSE](LICENSE)。
