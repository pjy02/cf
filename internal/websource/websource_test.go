package websource

import (
	"strings"
	"testing"

	"github.com/pjy02/cf/internal/model"
)

func TestParseWebHTML(t *testing.T) {
	data, err := Parse(strings.NewReader(sampleHTML()))
	if err != nil {
		t.Fatal(err)
	}
	ct := data.Carriers[model.CarrierCT]
	if !ct.Found || ct.SourceTime != "2026-07-21 16:07" || len(ct.Nodes) != 2 {
		t.Fatalf("unexpected ct snapshot: %#v", ct)
	}
	if got := ct.Nodes[0]; got.IP != "108.162.198.177" || got.Speed != 65.92 || got.Latency != 56.59 || got.Region != "东京" {
		t.Fatalf("unexpected first node: %#v", got)
	}
	cu := data.Carriers[model.CarrierCU]
	if !cu.Found || cu.SourceTime != "尚未同步数据" || len(cu.Nodes) != 0 {
		t.Fatalf("empty carrier must remain a valid empty panel: %#v", cu)
	}
	if _, ok := data.Carriers[model.CarrierCM]; ok {
		t.Fatal("missing cm panel must not be fabricated")
	}
	if len(data.Warnings) != 1 {
		t.Fatalf("expected one missing-panel warning, got %v", data.Warnings)
	}
}

func TestParseRejectsChallengePage(t *testing.T) {
	_, err := Parse(strings.NewReader(`<html><head><title>Just a moment...</title></head><body>challenge</body></html>`))
	if err == nil {
		t.Fatal("expected challenge page rejection")
	}
}

func TestParseRejectsCompletelyMalformedCards(t *testing.T) {
	html := `<html><head><title>CF 优选IP</title></head><body><div id="ct"><div class="card"><span class="ip-addr">1.1.1.1</span></div></div></body></html>`
	_, err := Parse(strings.NewReader(html))
	if err == nil {
		t.Fatal("expected malformed card rejection")
	}
}

func sampleHTML() string {
	return `<html><head><title>CF 优选IP</title></head><body>
<div id="ct" class="panel active"><div class="sync-info">中国电信 最后同步：<span>2026-07-21 16:07</span></div><div class="grid">
<div class="card"><span class="ip-addr">108.162.198.177</span><span class="region-tag">东京</span><div class="stat-box"><span class="stat-label">下载速度</span><span class="stat-value">65.92<small>MB/s</small></span></div><div class="stat-box"><span class="stat-label">网络延迟</span><span class="stat-value"><i></i>56.59<small>ms</small></span></div></div>
<div class="card"><span class="ip-addr">108.162.192.211</span><span class="region-tag">新加坡</span><div class="stat-box"><span class="stat-label">下载速度</span><span class="stat-value">61.90<small>MB/s</small></span></div><div class="stat-box"><span class="stat-label">网络延迟</span><span class="stat-value">62.44<small>ms</small></span></div></div>
</div></div>
<div id="cu" class="panel"><div class="sync-info">中国联通 最后同步：<span>尚未同步数据</span></div><div class="grid"></div></div>
</body></html>`
}
