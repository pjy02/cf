# Cloudflare 优选 IP 自动同步工具

在 Linux/SSH 环境中直接读取 [`https://ip.v2too.top/`](https://ip.v2too.top/) 的网页 HTML，筛选中国移动、中国联通、中国电信优选 IP，并自动同步为 Cloudflare DNS 多条 A 记录。

> 本项目只解析网站首页 HTML，不调用 `/api/nodes`。

## 功能

- 直接解析网页中的 `cm`、`cu`、`ct` 运营商面板。
- 默认同步 `cmcc`、`cucc`、`ctcc` 三个域名前缀。
- 每个运营商默认最多 3 条 A 记录。
- 只选择速度不低于该运营商最快节点 85% 的 IP。
- 运营商缺失时支持自动选择、固定借用指定运营商或禁止跨运营商借用。
- 借用中的域名每次同步都会优先跟随本轮网页结果更新，不会被已有旧 DNS 卡住。
- 页面异常、验证页或解析结构损坏时拒绝清空 DNS。
- Cloudflare 更新采用“先创建、后删除”的差异同步。
- SSH 中文操作面板，可设置自动同步间隔和筛选规则。
- systemd 定时执行，支持开机补跑和并发锁。
- 一键安装、更新覆盖和交互式卸载。
- Linux `amd64`、`arm64` 预编译发布，服务器不需要 Go 环境。

## 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/pjy02/cf/main/install.sh | sudo bash
```

指定版本：

```bash
curl -fsSL https://raw.githubusercontent.com/pjy02/cf/main/install.sh | sudo CFSYNC_VERSION=1.0.0 bash
```

安装脚本从 GitHub Releases 下载对应架构的二进制并校验 SHA256。SSH 有可用 TTY 时会直接进入首次配置；无 TTY 时会安全退出交互流程，并提示稍后运行：

```bash
sudo cf setup
sudo cf install-service
sudo cf sync
```

主命令安装为 `/usr/local/bin/cf`。为兼容旧版本，安装器会在不覆盖其他程序的前提下创建 `/usr/local/bin/cfsync -> /usr/local/bin/cf`。如果 `cf` 已被 Cloud Foundry CLI 等其他程序占用，安装器会停止并提示处理，不会强制覆盖。

## Cloudflare Token

推荐创建仅限目标 Zone 的 API Token：

- `Zone / Zone / Read`
- `Zone / DNS / Edit`

工具根据主域名自动查询 Zone ID。Token 只保存在 `/etc/cf-ip-sync/config.json`，文件权限为 `0600`。

## SSH 面板

```bash
sudo cf
```

菜单支持：

1. 立即同步。
2. 预览网页筛选和 DNS 差异，不做修改。
3. 修改 Token、域名和三个前缀。
4. 设置 `5m`、`30m`、`1h`、`1d` 等自动同步间隔。
5. 修改最大 A 记录数和速度比例。
6. 分别设置移动、联通、电信的借用策略。
7. 查看历史网页缓存、降级状态和错误。
8. 查看 systemd 日志。
9. 安装或修复定时器。
10. 卸载工具。

## 常用命令

```bash
# 打开面板
sudo cf

# 只验证首页 HTML 和默认 85% 筛选，不访问 Cloudflare
cf source

# 预览 DNS 变更
sudo cf preview

# 立即同步
sudo cf sync

# 查看定时器
systemctl status cf-ip-sync.timer

# 查看日志
journalctl -u cf-ip-sync.service -n 100 --no-pager

# 卸载（默认保留 Cloudflare DNS）
sudo cf uninstall
```

## 数据处理规则

网页面板与 DNS 前缀的默认映射：

| 网页面板 | 运营商 | DNS 主机名示例 |
|---|---|---|
| `cm` | 中国移动 | `cmcc.example.com` |
| `cu` | 中国联通 | `cucc.example.com` |
| `ct` | 中国电信 | `ctcc.example.com` |

每个运营商独立计算：

```text
最低合格速度 = 本运营商最快速度 × 0.85
```

合格节点按速度降序、延迟升序排列，去重后取前 3 个。三个主机名均使用 DNS Only（灰云）A 记录和 60 秒 TTL。

### 缺失运营商补齐顺序

1. 本轮网页中的本运营商结果。
2. 按该运营商的借用策略选择其他运营商本轮网页结果。
3. 本地保存的本运营商历史网页结果。
4. 按借用策略选择其他运营商历史网页结果。
5. Cloudflare 上该主机名已有的有效 A 记录。
6. 完全没有可用 IP 时跳过该主机名，不删除现有 DNS。

网页有某运营商结果时只更新该运营商缓存；空面板不会覆盖上一次有效缓存。借用策略发生在现有 DNS 之前，所以只要借用源本轮有新结果，目标域名每次同步都会一起更新。

每个运营商可以独立配置：

| 策略 | 行为 |
|---|---|
| `auto` | 自动借用本轮网页同步时间最新的其他运营商 |
| `cm` / `cu` / `ct` | 固定借用指定运营商 |
| `off` | 禁止跨运营商借用，仅使用自身结果、缓存或保留现有 DNS |

配置示例：

```json
"fallback": {
  "cm": "ct",
  "cu": "cm",
  "ct": "auto"
}
```

## 文件位置

```text
/usr/local/bin/cf
/usr/local/bin/cfsync  # 指向 cf 的兼容软链接
/etc/cf-ip-sync/config.json
/var/lib/cf-ip-sync/state.json
/etc/systemd/system/cf-ip-sync.service
/etc/systemd/system/cf-ip-sync.timer
```

程序会接管配置的 `cmcc`、`cucc`、`ctcc` 三个完整主机名下的全部 A 记录，不会修改其他主机名或其他类型的 DNS 记录。

## 卸载

```bash
sudo cf uninstall
```

可选择：

- 删除程序和 systemd 服务，保留配置与缓存。
- 完全删除本地程序、配置与缓存。

卸载默认保留 Cloudflare DNS，避免网站因卸载工具立即失去解析。

## 开发与验证

```bash
go test ./...
go vet ./...

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o cf ./cmd/cfsync
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o cf ./cmd/cfsync
```

Release 工作流从 GitHub Actions 手动运行，输入 `1.0.0` 或 `v1.0.0`，自动测试、交叉编译、生成校验文件并发布到 GitHub Releases。

## 许可证

[MIT](LICENSE)
