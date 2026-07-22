package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pjy02/cf/internal/cloudflare"
	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/fallback"
	"github.com/pjy02/cf/internal/lock"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/state"
	"github.com/pjy02/cf/internal/syncer"
	"github.com/pjy02/cf/internal/websource"
)

type Paths struct {
	Config string
	State  string
	Lock   string
}

func DefaultPaths() Paths {
	configPath := envOr("CFSYNC_CONFIG", "/etc/cf-ip-sync/config.json")
	statePath := envOr("CFSYNC_STATE", "/var/lib/cf-ip-sync/state.json")
	lockPath := envOr("CFSYNC_LOCK", "/var/lib/cf-ip-sync/sync.lock")
	return Paths{Config: configPath, State: statePath, Lock: lockPath}
}

type Service struct {
	Paths      Paths
	HTTPClient *http.Client
	SourceURL  string
	CFBaseURL  string
	Now        func() time.Time
}

type Report struct {
	ZoneID  string
	Source  model.SourceData
	Targets map[string]fallback.Target
	DNS     map[string]syncer.Report
	DryRun  bool
}

func New() *Service {
	return &Service{Paths: DefaultPaths(), Now: time.Now}
}

func (s *Service) Run(ctx context.Context, dryRun bool) (Report, error) {
	report := Report{Targets: make(map[string]fallback.Target), DNS: make(map[string]syncer.Report), DryRun: dryRun}
	cfg, err := config.Load(s.Paths.Config)
	if err != nil {
		return report, fmt.Errorf("加载配置: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Paths.Lock), 0o700); err != nil {
		return report, err
	}
	runLock, err := lock.Acquire(s.Paths.Lock)
	if err != nil {
		return report, err
	}
	defer runLock.Release()

	sourceClient := websource.NewClient()
	if s.HTTPClient != nil {
		sourceClient.HTTPClient = s.HTTPClient
	}
	if s.SourceURL != "" {
		sourceClient.URL = s.SourceURL
	}
	data, err := sourceClient.Fetch(ctx)
	if err != nil {
		s.recordFailure(err)
		return report, err
	}
	report.Source = data

	cf := cloudflare.New(cfg.APIToken)
	if s.HTTPClient != nil {
		cf.HTTPClient = s.HTTPClient
	}
	if s.CFBaseURL != "" {
		cf.BaseURL = s.CFBaseURL
	}
	zoneID := cfg.ZoneID
	if zoneID == "" {
		zoneID, err = cf.ResolveZone(ctx, cfg.Zone)
		if err != nil {
			s.recordFailure(err)
			return report, err
		}
		if !dryRun {
			cfg.ZoneID = zoneID
			if err := config.Save(s.Paths.Config, cfg); err != nil {
				return report, fmt.Errorf("保存 Zone ID: %w", err)
			}
		}
	}
	report.ZoneID = zoneID

	currentRecords := make(map[string][]cloudflare.Record)
	existingIPs := make(map[string][]string)
	for _, carrier := range model.CarrierOrder {
		records, listErr := cf.ListARecords(ctx, zoneID, cfg.Hostname(carrier))
		if listErr != nil {
			s.recordFailure(listErr)
			return report, listErr
		}
		currentRecords[carrier] = records
		for _, record := range records {
			existingIPs[carrier] = append(existingIPs[carrier], record.Content)
		}
	}

	cache, err := state.Load(s.Paths.State)
	if err != nil {
		return report, fmt.Errorf("加载状态缓存: %w", err)
	}
	cacheAge, _ := time.ParseDuration(cfg.CacheMaxAge)
	now := s.Now()
	report.Targets = fallback.Build(data, &cache, existingIPs, cfg.Fallback, cfg.SpeedRatio, cfg.MaxRecords, cacheAge, now)

	var syncErrors []error
	for _, carrier := range model.CarrierOrder {
		target := report.Targets[carrier]
		if len(target.Nodes) == 0 {
			continue
		}
		dnsReport, syncErr := syncer.Reconcile(ctx, cf, zoneID, cfg.Hostname(carrier), currentRecords[carrier], target.Nodes, cfg.TTL, dryRun)
		report.DNS[carrier] = dnsReport
		if syncErr != nil {
			syncErrors = append(syncErrors, fmt.Errorf("%s: %w", model.CarrierNames[carrier], syncErr))
		}
	}
	if dryRun {
		return report, errors.Join(syncErrors...)
	}
	cache.LastStatuses = make(map[string]string)
	for carrier, target := range report.Targets {
		cache.LastStatuses[carrier] = target.Mode + "：" + target.Detail
	}
	if len(syncErrors) == 0 {
		cache.LastSuccess = now
		cache.LastError = ""
	} else {
		cache.LastError = errors.Join(syncErrors...).Error()
	}
	if saveErr := state.Save(s.Paths.State, cache); saveErr != nil {
		syncErrors = append(syncErrors, fmt.Errorf("保存状态: %w", saveErr))
	}
	return report, errors.Join(syncErrors...)
}

func (s *Service) recordFailure(runErr error) {
	cache, err := state.Load(s.Paths.State)
	if err != nil {
		return
	}
	cache.LastError = runErr.Error()
	_ = state.Save(s.Paths.State, cache)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
