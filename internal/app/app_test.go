package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/fallback"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/state"
)

func TestRunUsesOnlyWebHTMLAndFillsMissingCarriers(t *testing.T) {
	var mu sync.Mutex
	created := 0
	apiNodesCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/nodes" {
			apiNodesCalled = true
			http.Error(w, "must not be called", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, integrationHTML())
			return
		}
		if strings.HasSuffix(r.URL.Path, "/dns_records") && r.Method == http.MethodGet {
			writeCF(w, []any{})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/dns_records") && r.Method == http.MethodPost {
			mu.Lock()
			created++
			id := created
			mu.Unlock()
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeCF(w, map[string]any{"id": fmt.Sprintf("record-%d", id), "type": "A", "name": body["name"], "content": body["content"]})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	dir := t.TempDir()
	paths := Paths{Config: filepath.Join(dir, "config.json"), State: filepath.Join(dir, "state.json"), Lock: filepath.Join(dir, "sync.lock")}
	cfg := config.Default()
	cfg.Zone = "example.com"
	cfg.ZoneID = "zone-id"
	cfg.APIToken = "test-token"
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 17, 0, 0, 0, time.Local)
	service := &Service{Paths: paths, HTTPClient: server.Client(), SourceURL: server.URL + "/", CFBaseURL: server.URL, Now: func() time.Time { return now }}
	report, err := service.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if apiNodesCalled {
		t.Fatal("/api/nodes was called")
	}
	if created != 3 {
		t.Fatalf("expected one A record per hostname, created %d", created)
	}
	if report.Targets[model.CarrierCT].Mode != fallback.ModeRealtime || report.Targets[model.CarrierCM].Mode != fallback.ModeBorrowed || report.Targets[model.CarrierCU].Mode != fallback.ModeBorrowed {
		t.Fatalf("unexpected fallback modes: %#v", report.Targets)
	}
	saved, err := state.Load(paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.LastSuccess.Equal(now) || len(saved.Carriers[model.CarrierCT].Nodes) != 1 {
		t.Fatalf("unexpected saved state: %#v", saved)
	}
}

func writeCF(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
}

func integrationHTML() string {
	return `<html><head><title>CF 优选IP</title></head><body>
<div id="ct"><div class="sync-info"><span>2026-07-21 16:07</span></div><div class="card"><span class="ip-addr">1.1.1.1</span><span class="region-tag">东京</span><div class="stat-box"><span class="stat-label">下载速度</span><span class="stat-value">60.00 MB/s</span></div><div class="stat-box"><span class="stat-label">网络延迟</span><span class="stat-value">55.00 ms</span></div></div></div>
<div id="cm"><div class="sync-info"><span>尚未同步数据</span></div></div>
<div id="cu"><div class="sync-info"><span>尚未同步数据</span></div></div>
</body></html>`
}
