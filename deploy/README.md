# 部署说明

tiliweb 是单二进制 + SQLite 的自托管 CMS，**不提供 Docker 部署**。推荐用 systemd 托管进程，nginx/Caddy 终止 TLS 并转发。

## 1. 构建与目录

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o tiliweb ./cmd/tiliweb
sudo install -m 0755 tiliweb /opt/tiliweb/tiliweb
sudo mkdir -p /etc/tiliweb /var/lib/tiliweb /var/log/tiliweb
sudo useradd --system --home /var/lib/tiliweb --shell /usr/sbin/nologin tiliweb
sudo chown -R tiliweb:tiliweb /var/lib/tiliweb /var/log/tiliweb
```

配置放在 `/etc/tiliweb/config.yaml`，数据库、媒体、备份放到 `/var/lib/tiliweb`。不要把 `config.yaml`、SQLite 文件放进 Web 根目录。

## 2. 初始化

```bash
sudo -u tiliweb /opt/tiliweb/tiliweb config check --config /etc/tiliweb/config.yaml
sudo -u tiliweb /opt/tiliweb/tiliweb migrate --config /etc/tiliweb/config.yaml
sudo -u tiliweb /opt/tiliweb/tiliweb admin create --config /etc/tiliweb/config.yaml --username admin --password '强密码'
```

## 3. systemd

```bash
sudo cp deploy/tiliweb.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now tiliweb
sudo systemctl status tiliweb
```

查看日志：

```bash
journalctl -u tiliweb -f
```

## 4. nginx

站点示例：`deploy/nginx-site.conf`；MCP 独立监听示例：`deploy/nginx-mcp.conf`。

- 站点默认只监听 `127.0.0.1:8080`，由 nginx 终止 TLS。
- `server.trusted_proxies` 必须包含 nginx 地址，否则真实 IP 和 `X-Forwarded-Proto` 不生效。
- 上传大文件时同时调整 `client_max_body_size` 和 `server.max_body_bytes`。
- 后台路径、`/contact` POST、`/mcp` 不要缓存；MCP 需要透传 `Authorization`，关闭响应缓冲。
- WAF 需放行 `Authorization` 头和 `application/json`；MCP 长任务超时要与 `mcp.http.write_timeout` 对齐。

## 5. MCP

```bash
sudo -u tiliweb /opt/tiliweb/tiliweb mcp token create \
  --config /etc/tiliweb/config.yaml \
  --name ai-ops --scopes read --ttl 720h
```

只读令牌只给 `read`；写操作需要 `mcp.allow_write=true` 和对应作用域；`admin` 令牌还需要 `mcp.allow_admin_scope=true`。令牌只在创建时显示一次，之后只能看前缀。

非 loopback 暴露必须开启 `mcp.http.tls`；走反代时让 `mcp.http.public=true` 并确认代理已终止 TLS。默认只监听 `127.0.0.1:8090`，不要直接暴露到公网。

## 6. 备份

```bash
# 数据库快照
sudo -u tiliweb /opt/tiliweb/tiliweb backup --config /etc/tiliweb/config.yaml

# 数据库 + 媒体/附件 zip
sudo -u tiliweb /opt/tiliweb/tiliweb backup --config /etc/tiliweb/config.yaml --with-media
```

建议 cron 每日执行并复制到另一台机器；备份目录不要放在可公开访问的路径。

## 7. 安全清单

- 生产必须 HTTPS，`base_url` 用 https，`cookie_secure=true`。
- 后台路径可改 `admin.path`，不要使用默认弱口令。
- 关闭不需要的注册模式；审核开启时新用户先审批。
- 限制 `mcp.http.allowed_ips`，令牌短 TTL，定期吊销。
- 不给 `/data`、`/backups`、`config.yaml` 任何 Web 访问权限。
- 升级前先 `tiliweb backup`；迁移失败时保留旧二进制和数据库快照。
