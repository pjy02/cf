package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/pjy02/cf/internal/app"
	"github.com/pjy02/cf/internal/cloudflare"
	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/state"
	"github.com/pjy02/cf/internal/systemd"
	"github.com/pjy02/cf/internal/updater"
)

type Panel struct {
	Console *Console
	Service *app.Service
	Version string
}

func New(version string) *Panel {
	return &Panel{Console: OpenConsole(), Service: app.New(), Version: version}
}

func (p *Panel) Close() { p.Console.Close() }

func (p *Panel) Run(ctx context.Context) error {
	if _, err := os.Stat(p.Service.Paths.Config); errors.Is(err, os.ErrNotExist) {
		p.Console.Printf("首次运行需要完成 Cloudflare 配置。\n\n")
		if err := p.Setup(ctx); err != nil {
			return err
		}
	}
	for {
		p.Console.Clear()
		p.drawHeader()
		p.Console.Printf(`
1. 立即同步
2. 预览网页筛选和 DNS 变更
3. 设置 Cloudflare Token、域名和前缀
4. 设置自动同步间隔
5. 设置筛选规则
6. 设置各运营商借用策略
7. 查看运行状态和缓存
8. 查看最近日志
9. 检查更新
10. 安装或修复 systemd 定时器
11. 卸载工具
0. 退出

`)
		choice, err := p.Console.ReadLine("请选择 [0-11]：")
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch choice {
		case "1":
			p.runSync(ctx, false)
		case "2":
			p.runSync(ctx, true)
		case "3":
			if err := p.Setup(ctx); err != nil {
				p.Console.Printf("\n配置失败：%v\n", err)
			}
			p.Console.Pause()
		case "4":
			p.setInterval()
		case "5":
			p.setSelection()
		case "6":
			p.setFallback()
		case "7":
			p.showStatus()
		case "8":
			p.showLogs()
		case "9":
			updated, updateErr := p.checkUpdate(ctx, false)
			if updateErr != nil {
				p.Console.Printf("\n检查更新失败：%v\n", updateErr)
				p.Console.Pause()
			} else if updated {
				return nil
			}
		case "10":
			p.installService()
		case "11":
			removed, removeErr := p.uninstall()
			if removeErr != nil {
				p.Console.Printf("\n卸载失败：%v\n", removeErr)
				p.Console.Pause()
			} else if removed {
				return nil
			}
		case "0":
			return nil
		default:
			p.Console.Printf("\n无效选项。\n")
			p.Console.Pause()
		}
	}
}

func (p *Panel) Setup(ctx context.Context) error {
	cfg := config.Default()
	if loaded, err := config.Load(p.Service.Paths.Config); err == nil {
		cfg = loaded
	}
	p.Console.Printf("数据源固定为：%s（直接读取 Web HTML）\n", config.SourceURL)
	zone, err := p.promptDefault("Cloudflare 主域名", cfg.Zone)
	if err != nil {
		return err
	}
	cfg.Zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
	tokenPrompt := "Cloudflare API Token"
	if cfg.APIToken != "" {
		tokenPrompt += "（直接回车保留现有 Token）"
	}
	token, err := p.Console.ReadSecret(tokenPrompt + "：")
	if err != nil {
		return err
	}
	if token != "" {
		cfg.APIToken = token
		cfg.ZoneID = ""
	}
	for _, carrier := range model.CarrierOrder {
		value, promptErr := p.promptDefault(model.CarrierNames[carrier]+"域名前缀", cfg.Prefixes[carrier])
		if promptErr != nil {
			return promptErr
		}
		cfg.Prefixes[carrier] = strings.ToLower(value)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	p.Console.Printf("\n正在验证 Token 和域名...")
	client := cloudflare.New(cfg.APIToken)
	if err := client.VerifyToken(ctx); err != nil {
		return err
	}
	zoneID, err := client.ResolveZone(ctx, cfg.Zone)
	if err != nil {
		return err
	}
	cfg.ZoneID = zoneID
	if err := config.Save(p.Service.Paths.Config, cfg); err != nil {
		return err
	}
	p.Console.Printf(" 成功。\n配置已保存，工具将接管以下主机名的全部 A 记录：\n")
	for _, carrier := range model.CarrierOrder {
		p.Console.Printf("  - %s\n", cfg.Hostname(carrier))
	}
	return nil
}

func (p *Panel) runSync(ctx context.Context, dryRun bool) {
	p.Console.Printf("\n正在%s，请稍候...\n", map[bool]string{true: "预览", false: "同步"}[dryRun])
	report, err := p.Service.Run(ctx, dryRun)
	PrintReport(p.Console, report)
	if err != nil {
		p.Console.Printf("\n执行失败：%v\n", err)
	} else if dryRun {
		p.Console.Printf("\n预览完成，未修改 Cloudflare DNS。\n")
	} else {
		p.Console.Printf("\n同步完成。\n")
	}
	p.Console.Pause()
}

func PrintReport(out interface{ Printf(string, ...any) }, report app.Report) {
	for _, warning := range report.Source.Warnings {
		out.Printf("警告：%s\n", warning)
	}
	for _, carrier := range model.CarrierOrder {
		target := report.Targets[carrier]
		out.Printf("\n%s：%s｜%s\n", model.CarrierNames[carrier], target.Mode, target.Detail)
		for _, node := range target.Nodes {
			out.Printf("  %-15s %6.2f MB/s %6.2f ms %s\n", node.IP, node.Speed, node.Latency, node.Region)
		}
		if dns, ok := report.DNS[carrier]; ok {
			out.Printf("  DNS：新增 %d，更新 %d，删除 %d，保留 %d\n", dns.Created, dns.Updated, dns.Deleted, dns.Unchanged)
		}
	}
}

func (p *Panel) drawHeader() {
	cfg, cfgErr := config.Load(p.Service.Paths.Config)
	cache, _ := state.Load(p.Service.Paths.State)
	p.Console.Printf("━━━━━━━━ Cloudflare 优选 IP 同步工具 v%s ━━━━━━━━\n", p.Version)
	p.Console.Printf("数据源：%s（Web HTML）\n", config.SourceURL)
	if cfgErr != nil {
		p.Console.Printf("配置状态：%v\n", cfgErr)
		return
	}
	p.Console.Printf("目标域名：%s｜间隔：%s｜最多：%d 个｜速度比例：%.0f%%\n", cfg.Zone, cfg.Interval, cfg.MaxRecords, cfg.SpeedRatio*100)
	p.Console.Printf("借用策略：移动=%s｜联通=%s｜电信=%s\n",
		fallbackDescription(cfg.Fallback[model.CarrierCM]),
		fallbackDescription(cfg.Fallback[model.CarrierCU]),
		fallbackDescription(cfg.Fallback[model.CarrierCT]))
	if runtime.GOOS == "linux" {
		p.Console.Printf("定时器：%s\n", systemd.TimerStatus())
	}
	if !cache.LastSuccess.IsZero() {
		p.Console.Printf("上次成功：%s\n", cache.LastSuccess.Format("2006-01-02 15:04:05"))
	}
	if cache.LastError != "" {
		p.Console.Printf("最近错误：%s\n", cache.LastError)
	}
	for _, carrier := range model.CarrierOrder {
		if status := cache.LastStatuses[carrier]; status != "" {
			p.Console.Printf("%s：%s\n", model.CarrierNames[carrier], status)
		}
	}
}

func (p *Panel) setInterval() {
	cfg, err := config.Load(p.Service.Paths.Config)
	if err != nil {
		p.Console.Printf("\n%v\n", err)
		p.Console.Pause()
		return
	}
	value, err := p.promptDefault("同步间隔（如 5m、30m、1h）", cfg.Interval)
	if err != nil {
		return
	}
	if _, err := config.ParseInterval(value); err != nil {
		p.Console.Printf("\n%v\n", err)
		p.Console.Pause()
		return
	}
	cfg.Interval = strings.ToLower(strings.TrimSpace(value))
	if err := config.Save(p.Service.Paths.Config, cfg); err != nil {
		p.Console.Printf("\n保存失败：%v\n", err)
	} else if runtime.GOOS == "linux" && systemd.IsInstalled() {
		binary, _ := os.Executable()
		if err := systemd.Install(binary, cfg.Interval); err != nil {
			p.Console.Printf("\n配置已保存，但重载定时器失败：%v\n", err)
		} else {
			p.Console.Printf("\n同步间隔已更新为 %s。\n", cfg.Interval)
		}
	} else {
		p.Console.Printf("\n同步间隔已保存。\n")
	}
	p.Console.Pause()
}

func (p *Panel) setSelection() {
	cfg, err := config.Load(p.Service.Paths.Config)
	if err != nil {
		p.Console.Printf("\n%v\n", err)
		p.Console.Pause()
		return
	}
	maxValue, err := p.promptDefault("每个运营商最多 A 记录数", strconv.Itoa(cfg.MaxRecords))
	if err != nil {
		return
	}
	maxRecords, err := strconv.Atoi(maxValue)
	if err != nil {
		p.Console.Printf("\n记录数必须是整数。\n")
		p.Console.Pause()
		return
	}
	ratioValue, err := p.promptDefault("最低速度百分比", strconv.Itoa(int(cfg.SpeedRatio*100)))
	if err != nil {
		return
	}
	ratio, err := strconv.ParseFloat(ratioValue, 64)
	if err != nil {
		p.Console.Printf("\n速度比例必须是数字。\n")
		p.Console.Pause()
		return
	}
	cfg.MaxRecords = maxRecords
	cfg.SpeedRatio = ratio / 100
	if err := config.Save(p.Service.Paths.Config, cfg); err != nil {
		p.Console.Printf("\n保存失败：%v\n", err)
	} else {
		p.Console.Printf("\n筛选规则已保存。\n")
	}
	p.Console.Pause()
}

func (p *Panel) setFallback() {
	cfg, err := config.Load(p.Service.Paths.Config)
	if err != nil {
		p.Console.Printf("\n%v\n", err)
		p.Console.Pause()
		return
	}
	for _, carrier := range model.CarrierOrder {
		others := otherCarriers(carrier)
		p.Console.Printf("\n%s无网页数据时，当前策略：%s\n", model.CarrierNames[carrier], fallbackDescription(cfg.Fallback[carrier]))
		p.Console.Printf("  1. 自动选择本轮更新时间最新的运营商\n")
		p.Console.Printf("  2. 固定借用%s\n", model.CarrierNames[others[0]])
		p.Console.Printf("  3. 固定借用%s\n", model.CarrierNames[others[1]])
		p.Console.Printf("  4. 禁止跨运营商借用\n")
		choice, readErr := p.Console.ReadLine("请选择 [1-4]，直接回车保持不变：")
		if readErr != nil {
			return
		}
		switch choice {
		case "":
		case "1":
			cfg.Fallback[carrier] = config.FallbackAuto
		case "2":
			cfg.Fallback[carrier] = others[0]
		case "3":
			cfg.Fallback[carrier] = others[1]
		case "4":
			cfg.Fallback[carrier] = config.FallbackOff
		default:
			p.Console.Printf("无效选项，%s策略保持不变。\n", model.CarrierNames[carrier])
		}
	}
	if err := config.Save(p.Service.Paths.Config, cfg); err != nil {
		p.Console.Printf("\n保存失败：%v\n", err)
	} else {
		p.Console.Printf("\n三个运营商的借用策略已保存，下次同步立即生效。\n")
	}
	p.Console.Pause()
}

func otherCarriers(carrier string) []string {
	result := make([]string, 0, 2)
	for _, candidate := range model.CarrierOrder {
		if candidate != carrier {
			result = append(result, candidate)
		}
	}
	return result
}

func fallbackDescription(strategy string) string {
	switch strategy {
	case config.FallbackOff:
		return "禁止借用"
	case config.FallbackAuto, "":
		return "自动"
	default:
		if name, ok := model.CarrierNames[strategy]; ok {
			return "固定" + name
		}
		return "未知"
	}
}

func (p *Panel) showStatus() {
	p.Console.Clear()
	p.drawHeader()
	cache, err := state.Load(p.Service.Paths.State)
	if err != nil {
		p.Console.Printf("\n读取缓存失败：%v\n", err)
	} else {
		for _, carrier := range model.CarrierOrder {
			entry, ok := cache.Carriers[carrier]
			if !ok {
				p.Console.Printf("\n%s：暂无网页缓存\n", model.CarrierNames[carrier])
				continue
			}
			p.Console.Printf("\n%s：网页时间 %s，抓取于 %s，共 %d 个节点\n", model.CarrierNames[carrier], entry.SourceTime, entry.FetchedAt.Format("2006-01-02 15:04:05"), len(entry.Nodes))
		}
	}
	p.Console.Pause()
}

func (p *Panel) showLogs() {
	if runtime.GOOS != "linux" {
		p.Console.Printf("\n日志查看仅支持 systemd Linux。\n")
		p.Console.Pause()
		return
	}
	cmd := exec.Command("journalctl", "-u", systemd.ServiceName, "-n", "100", "--no-pager")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	p.Console.Pause()
}

func (p *Panel) installService() {
	if runtime.GOOS != "linux" {
		p.Console.Printf("\nsystemd 定时器只能安装在 Linux。\n")
		p.Console.Pause()
		return
	}
	cfg, err := config.Load(p.Service.Paths.Config)
	if err != nil {
		p.Console.Printf("\n%v\n", err)
		p.Console.Pause()
		return
	}
	binary, _ := os.Executable()
	if err := systemd.Install(binary, cfg.Interval); err != nil {
		p.Console.Printf("\n安装失败：%v\n", err)
	} else {
		p.Console.Printf("\nsystemd 定时器已安装并启动。\n")
	}
	p.Console.Pause()
}

func (p *Panel) checkUpdate(ctx context.Context, checkOnly bool) (bool, error) {
	p.Console.Printf("\n正在检查 GitHub 最新版本...\n")
	client := updater.NewClient()
	info, err := client.Check(ctx, p.Version)
	if err != nil {
		return false, err
	}
	p.Console.Printf("\n━━━━━━━━ 检查更新 ━━━━━━━━\n")
	p.Console.Printf("当前版本：%s\n", info.CurrentVersion)
	p.Console.Printf("最新版本：%s\n", info.LatestVersion)
	if !info.Release.PublishedAt.IsZero() {
		p.Console.Printf("发布时间：%s\n", info.Release.PublishedAt.Local().Format("2006-01-02 15:04:05"))
	}
	if info.Release.HTMLURL != "" {
		p.Console.Printf("更新地址：%s\n", updater.CleanReleaseNotes(info.Release.HTMLURL, 500))
	}
	if info.Development {
		p.Console.Printf("状态：当前是开发版本，可安装最新正式版本。\n")
	} else if info.Ahead {
		p.Console.Printf("状态：当前版本高于最新正式版本，不执行降级。\n")
	} else if !info.UpdateAvailable {
		p.Console.Printf("状态：当前已经是最新版本。\n")
	} else {
		p.Console.Printf("状态：发现可用更新。\n")
	}
	p.Console.Printf("\n更新内容：\n%s\n", updater.CleanReleaseNotes(info.Release.Body, 2000))
	if checkOnly || !info.UpdateAvailable || info.Ahead {
		return false, nil
	}
	if runtime.GOOS != "linux" {
		return false, fmt.Errorf("自动安装更新仅支持 Linux")
	}
	choice, err := p.Console.ReadLine("\n输入 UPDATE 确认下载并安装，直接回车取消：")
	if err != nil {
		return false, err
	}
	if choice != "UPDATE" {
		p.Console.Printf("已取消更新。\n")
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	p.Console.Printf("正在下载、校验并安装 %s...\n", info.LatestVersion)
	if err := client.Install(ctx, info, executable); err != nil {
		var warning *updater.CleanupWarning
		if !errors.As(err, &warning) {
			return false, err
		}
		p.Console.Printf("警告：%v\n", warning)
	}
	p.Console.Printf("\n更新成功：%s → %s\n", info.CurrentVersion, info.LatestVersion)
	p.Console.Printf("请重新运行 cf 进入新版本。\n")
	p.Console.Pause()
	return true, nil
}

func (p *Panel) uninstall() (bool, error) {
	p.Console.Printf("\n卸载不会删除 Cloudflare 上现有 DNS 记录。\n")
	p.Console.Printf("1. 卸载程序，保留配置和缓存\n2. 完全卸载本地文件\n0. 取消\n")
	choice, err := p.Console.ReadLine("请选择：")
	if err != nil || choice == "0" || choice == "" {
		return false, err
	}
	if choice != "1" && choice != "2" {
		return false, fmt.Errorf("无效卸载选项")
	}
	confirm, err := p.Console.ReadLine("输入 UNINSTALL 确认卸载：")
	if err != nil {
		return false, err
	}
	if confirm != "UNINSTALL" {
		p.Console.Printf("已取消卸载。\n")
		p.Console.Pause()
		return false, nil
	}
	if runtime.GOOS == "linux" && systemd.IsInstalled() {
		if err := systemd.Remove(); err != nil {
			return false, err
		}
	}
	if choice == "2" {
		_ = os.Remove(p.Service.Paths.Config)
		_ = os.Remove(p.Service.Paths.State)
		_ = os.Remove(filepath.Dir(p.Service.Paths.Config))
		_ = os.Remove(filepath.Dir(p.Service.Paths.State))
	}
	binary, _ := os.Executable()
	removeCompatibilityAliases(binary)
	if err := os.Remove(binary); err != nil {
		return false, fmt.Errorf("删除程序 %s: %w", binary, err)
	}
	p.Console.Printf("卸载完成，Cloudflare DNS 记录已保留。\n")
	return true, nil
}

func removeCompatibilityAliases(binary string) {
	resolvedBinary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		resolvedBinary = binary
	}
	for _, alias := range []string{"/usr/local/bin/cf", "/usr/local/bin/cfsync"} {
		if alias == binary {
			continue
		}
		info, err := os.Lstat(alias)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		resolvedAlias, err := filepath.EvalSymlinks(alias)
		if err == nil && resolvedAlias == resolvedBinary {
			_ = os.Remove(alias)
		}
	}
}

func (p *Panel) promptDefault(label, current string) (string, error) {
	prompt := label + "："
	if current != "" {
		prompt = fmt.Sprintf("%s [%s]：", label, current)
	}
	value, err := p.Console.ReadLine(prompt)
	if err != nil {
		return "", err
	}
	if value == "" {
		return current, nil
	}
	return value, nil
}

func InstallServiceFromConfig(paths app.Paths) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd 安装只支持 Linux")
	}
	cfg, err := config.Load(paths.Config)
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	return systemd.Install(binary, cfg.Interval)
}

func RunUninstall(version string) error {
	p := New(version)
	defer p.Close()
	_, err := p.uninstall()
	return err
}

func RunUpdate(version string, checkOnly bool) error {
	p := New(version)
	defer p.Close()
	_, err := p.checkUpdate(context.Background(), checkOnly)
	return err
}

func PrintCLIReport(report app.Report) {
	out := &stdoutPrinter{}
	PrintReport(out, report)
}

type stdoutPrinter struct{}

func (*stdoutPrinter) Printf(format string, args ...any) { fmt.Printf(format, args...) }

func QuietSuccess(report app.Report) string {
	parts := make([]string, 0, 3)
	for _, carrier := range model.CarrierOrder {
		target := report.Targets[carrier]
		parts = append(parts, fmt.Sprintf("%s=%d(%s)", carrier, len(target.Nodes), target.Mode))
	}
	return strings.Join(parts, " ")
}
