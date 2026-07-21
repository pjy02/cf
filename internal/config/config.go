package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pjy02/cf/internal/model"
)

const SourceURL = "https://ip.v2too.top/"

type Config struct {
	Zone        string            `json:"zone"`
	ZoneID      string            `json:"zone_id,omitempty"`
	APIToken    string            `json:"api_token"`
	MaxRecords  int               `json:"max_records"`
	SpeedRatio  float64           `json:"speed_ratio"`
	TTL         int               `json:"ttl"`
	Interval    string            `json:"interval"`
	CacheMaxAge string            `json:"cache_max_age"`
	Prefixes    map[string]string `json:"prefixes"`
}

func Default() Config {
	return Config{
		MaxRecords:  3,
		SpeedRatio:  0.85,
		TTL:         60,
		Interval:    "30m",
		CacheMaxAge: "72h",
		Prefixes: map[string]string{
			model.CarrierCM: "cmcc",
			model.CarrierCU: "cucc",
			model.CarrierCT: "ctcc",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件: %w", err)
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

var prefixRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func (c Config) Validate() error {
	zone := strings.TrimSpace(strings.TrimSuffix(c.Zone, "."))
	if zone == "" {
		return errors.New("尚未设置 Cloudflare 域名")
	}
	if strings.ContainsAny(zone, "/:* ") || !strings.Contains(zone, ".") {
		return errors.New("Cloudflare 主域名格式无效，例如应填写 example.com")
	}
	for _, label := range strings.Split(zone, ".") {
		if !prefixRE.MatchString(label) || strings.HasSuffix(label, "-") {
			return errors.New("Cloudflare 主域名包含无效标签")
		}
	}
	if c.APIToken == "" {
		return errors.New("尚未设置 Cloudflare API Token")
	}
	if c.MaxRecords < 1 || c.MaxRecords > 20 {
		return errors.New("最大记录数必须在 1 到 20 之间")
	}
	if c.SpeedRatio <= 0 || c.SpeedRatio > 1 {
		return errors.New("速度比例必须大于 0 且不超过 1")
	}
	if c.TTL != 1 && (c.TTL < 60 || c.TTL > 86400) {
		return errors.New("TTL 必须为 1，或在 60 到 86400 秒之间")
	}
	if _, err := ParseInterval(c.Interval); err != nil {
		return err
	}
	if _, err := time.ParseDuration(c.CacheMaxAge); err != nil {
		return fmt.Errorf("缓存时效无效: %w", err)
	}
	seen := map[string]bool{}
	for _, carrier := range model.CarrierOrder {
		prefix := strings.TrimSpace(strings.ToLower(c.Prefixes[carrier]))
		if !prefixRE.MatchString(prefix) {
			return fmt.Errorf("%s 域名前缀无效", model.CarrierNames[carrier])
		}
		if seen[prefix] {
			return errors.New("三个运营商的域名前缀不能重复")
		}
		seen[prefix] = true
	}
	return nil
}

func ParseInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasSuffix(value, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(value, "d")); err == nil {
			value = fmt.Sprintf("%dh", days*24)
		}
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < time.Minute {
		return 0, errors.New("同步间隔必须是至少 1 分钟的时长，例如 5m、30m、1h")
	}
	return d, nil
}

func (c *Config) normalize() {
	c.Zone = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(c.Zone, ".")))
	c.APIToken = strings.TrimSpace(c.APIToken)
	c.Interval = strings.ToLower(strings.TrimSpace(c.Interval))
	for carrier, prefix := range c.Prefixes {
		c.Prefixes[carrier] = strings.ToLower(strings.TrimSpace(prefix))
	}
}

func (c Config) Hostname(carrier string) string {
	return strings.ToLower(c.Prefixes[carrier] + "." + strings.TrimSuffix(c.Zone, "."))
}
