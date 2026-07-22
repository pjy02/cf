package fallback

import (
	"testing"
	"time"

	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/state"
)

func TestBorrowedCarrierRefreshesEveryRunInsteadOfKeepingExistingDNS(t *testing.T) {
	now := time.Date(2026, 7, 22, 16, 30, 0, 0, time.Local)
	cache := state.Empty()
	strategies := config.Default().Fallback

	first := sourceWith(model.CarrierCT, "1.1.1.1", 60, "2026-07-22 16:08", now)
	firstTargets := Build(first, &cache, map[string][]string{}, strategies, 0.85, 3, 72*time.Hour, now)
	if got := firstTargets[model.CarrierCU]; got.Mode != ModeBorrowed || got.Nodes[0].IP != "1.1.1.1" {
		t.Fatalf("first sync did not borrow ct: %#v", got)
	}

	second := sourceWith(model.CarrierCT, "8.8.8.8", 80, "2026-07-22 16:28", now.Add(20*time.Minute))
	existing := map[string][]string{model.CarrierCU: {"1.1.1.1"}}
	secondTargets := Build(second, &cache, existing, strategies, 0.85, 3, 72*time.Hour, now.Add(20*time.Minute))
	if got := secondTargets[model.CarrierCU]; got.Mode != ModeBorrowed || got.Nodes[0].IP != "8.8.8.8" {
		t.Fatalf("second sync kept stale existing DNS instead of fresh donor: %#v", got)
	}
}

func TestFixedBorrowStrategyIsIndependentPerCarrier(t *testing.T) {
	now := time.Now()
	data := model.SourceData{FetchedAt: now, Carriers: map[string]model.Snapshot{
		model.CarrierCT: {Carrier: model.CarrierCT, Found: true, SourceTime: "2026-07-22 16:08", Nodes: []model.Node{{IP: "1.1.1.1", Speed: 90, Carrier: model.CarrierCT}}},
		model.CarrierCM: {Carrier: model.CarrierCM, Found: true, SourceTime: "2026-07-22 06:06", Nodes: []model.Node{{IP: "8.8.8.8", Speed: 50, Carrier: model.CarrierCM}}},
	}}
	cache := state.Empty()
	strategies := map[string]string{
		model.CarrierCM: config.FallbackAuto,
		model.CarrierCU: model.CarrierCM,
		model.CarrierCT: config.FallbackAuto,
	}
	targets := Build(data, &cache, map[string][]string{}, strategies, 0.85, 3, 72*time.Hour, now)
	if got := targets[model.CarrierCU]; got.Donor != model.CarrierCM || got.Nodes[0].IP != "8.8.8.8" {
		t.Fatalf("cu did not follow fixed cm strategy: %#v", got)
	}
}

func TestOffStrategyNeverBorrowsOtherCarrier(t *testing.T) {
	now := time.Now()
	data := sourceWith(model.CarrierCT, "1.1.1.1", 100, "2026-07-22 16:08", now)
	cache := state.Empty()
	strategies := config.Default().Fallback
	strategies[model.CarrierCU] = config.FallbackOff
	targets := Build(data, &cache, map[string][]string{model.CarrierCU: {"9.9.9.9"}}, strategies, 0.85, 3, 72*time.Hour, now)
	if got := targets[model.CarrierCU]; got.Mode != ModeRetained || got.Nodes[0].IP != "9.9.9.9" {
		t.Fatalf("off strategy unexpectedly borrowed: %#v", got)
	}
}

func TestOwnCacheIsUsedWhenNoCurrentDonorExists(t *testing.T) {
	now := time.Now()
	cache := state.Empty()
	cache.Carriers[model.CarrierCU] = state.CacheEntry{FetchedAt: now.Add(-time.Hour), Nodes: []model.Node{{IP: "9.9.9.9", Speed: 50, Carrier: model.CarrierCU}}}
	data := model.SourceData{FetchedAt: now, Carriers: map[string]model.Snapshot{
		model.CarrierCU: {Carrier: model.CarrierCU, Found: true},
	}}
	targets := Build(data, &cache, map[string][]string{}, config.Default().Fallback, 0.85, 3, 72*time.Hour, now)
	if got := targets[model.CarrierCU]; got.Mode != ModeCache || got.Nodes[0].IP != "9.9.9.9" {
		t.Fatalf("own cache was not used: %#v", got)
	}
}

func sourceWith(carrier, ip string, speed float64, sourceTime string, fetchedAt time.Time) model.SourceData {
	return model.SourceData{FetchedAt: fetchedAt, Carriers: map[string]model.Snapshot{
		carrier: {Carrier: carrier, Found: true, SourceTime: sourceTime, Nodes: []model.Node{{IP: ip, Speed: speed, Carrier: carrier}}},
	}}
}
