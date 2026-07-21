package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pjy02/cf/internal/config"
)

const (
	ServiceName = "cf-ip-sync.service"
	TimerName   = "cf-ip-sync.timer"
	unitDir     = "/etc/systemd/system"
)

func RenderService(binary string) string {
	return fmt.Sprintf(`[Unit]
Description=Cloudflare preferred IP DNS synchronization
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=root
ExecStart=%s sync --quiet
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/etc/cf-ip-sync /var/lib/cf-ip-sync

`, strconv.Quote(binary))
}

func RenderTimer(interval string) (string, error) {
	if _, err := config.ParseInterval(interval); err != nil {
		return "", err
	}
	return fmt.Sprintf(`[Unit]
Description=Periodically synchronize Cloudflare preferred IP DNS records

[Timer]
OnBootSec=2min
OnUnitActiveSec=%s
RandomizedDelaySec=30s
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, interval, ServiceName), nil
}

func Install(binary, interval string) error {
	timer, err := RenderTimer(interval)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(binary) {
		return fmt.Errorf("systemd 服务需要二进制绝对路径: %s", binary)
	}
	if err := os.MkdirAll("/etc/cf-ip-sync", 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll("/var/lib/cf-ip-sync", 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(unitDir, ServiceName), []byte(RenderService(binary)), 0o644); err != nil {
		return fmt.Errorf("写入 systemd service: %w", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, TimerName), []byte(timer), 0o644); err != nil {
		return fmt.Errorf("写入 systemd timer: %w", err)
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return run("systemctl", "enable", "--now", TimerName)
}

func Remove() error {
	_ = run("systemctl", "disable", "--now", TimerName)
	var errs []string
	for _, name := range []string{ServiceName, TimerName} {
		if err := os.Remove(filepath.Join(unitDir, name)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if err := run("systemctl", "daemon-reload"); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("卸载 systemd 服务: %s", strings.Join(errs, "; "))
	}
	return nil
}

func IsInstalled() bool {
	_, err := os.Stat(filepath.Join(unitDir, TimerName))
	return err == nil
}

func TimerStatus() string {
	out, err := exec.Command("systemctl", "is-active", TimerName).CombinedOutput()
	if err != nil {
		value := strings.TrimSpace(string(out))
		if value == "" {
			return "未运行"
		}
		return value
	}
	return strings.TrimSpace(string(out))
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s 失败: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
