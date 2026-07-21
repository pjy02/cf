package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/pjy02/cf/internal/app"
	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/panel"
	"github.com/pjy02/cf/internal/selector"
	"github.com/pjy02/cf/internal/websource"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run() error {
	command := "panel"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch command {
	case "panel":
		p := panel.New(version)
		defer p.Close()
		return p.Run(context.Background())
	case "setup":
		p := panel.New(version)
		defer p.Close()
		return p.Setup(ctx)
	case "sync", "preview":
		flags := flag.NewFlagSet(command, flag.ContinueOnError)
		dryRun := flags.Bool("dry-run", command == "preview", "preview without changing DNS")
		quiet := flags.Bool("quiet", false, "only print a compact result")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		report, err := app.New().Run(ctx, *dryRun)
		if !*quiet {
			panel.PrintCLIReport(report)
		} else if err == nil {
			fmt.Println(panel.QuietSuccess(report))
		}
		return err
	case "install-service":
		return panel.InstallServiceFromConfig(app.DefaultPaths())
	case "source":
		data, err := websource.NewClient().Fetch(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("数据源：%s（Web HTML）\n抓取时间：%s\n", config.SourceURL, data.FetchedAt.Format("2006-01-02 15:04:05"))
		for _, warning := range data.Warnings {
			fmt.Printf("警告：%s\n", warning)
		}
		defaults := config.Default()
		for _, carrier := range model.CarrierOrder {
			snapshot, ok := data.Carriers[carrier]
			if !ok {
				fmt.Printf("%s：网页面板缺失\n", model.CarrierNames[carrier])
				continue
			}
			selected := selector.Select(snapshot.Nodes, defaults.SpeedRatio, defaults.MaxRecords)
			fmt.Printf("%s：网页节点 %d，85%% 筛选后 %d，网页时间 %s\n", model.CarrierNames[carrier], len(snapshot.Nodes), len(selected), snapshot.SourceTime)
			for _, node := range selected {
				fmt.Printf("  %-15s %6.2f MB/s %6.2f ms %s\n", node.IP, node.Speed, node.Latency, node.Region)
			}
		}
		return nil
	case "uninstall":
		return panel.RunUninstall(version)
	case "version", "--version", "-v":
		fmt.Printf("cfsync %s (commit %s, built %s)\n", version, commit, date)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		return fmt.Errorf("未知命令 %q，运行 cfsync help 查看帮助", command)
	}
}

func printHelp() {
	fmt.Print(`cfsync - Cloudflare 优选 IP 自动同步工具

用法：
  cfsync                    打开 SSH 管理面板
  cfsync setup              配置 Token、域名和前缀
  cfsync sync               立即同步
  cfsync preview            预览但不修改 DNS
  cfsync source             只读取并检查网页 HTML，不访问 Cloudflare
  cfsync install-service    安装或修复 systemd 定时器
  cfsync uninstall          卸载工具（默认保留 DNS）
  cfsync version            查看版本
`)
}
