package fallback

import (
	"testing"
	"time"

	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/state"
)

func TestBuildUsesRealtimeCacheAndBorrowing(t *testing.T) {
	now := time.Date(2026, 7, 21, 17, 0, 0, 0, time.Local)
	data := model.SourceData{FetchedAt: now, Carriers: map[string]model.Snapshot{
		model.CarrierCT: {Carrier: model.CarrierCT, Found: true, SourceTime: "2026-07-21 16:07", Nodes: []model.Node{{IP: "1.1.1.1", Speed: 100, Carrier: model.CarrierCT}}},
	}}
	cache := state.Empty()
	cache.Carriers[model.CarrierCM] = state.CacheEntry{FetchedAt: now.Add(-time.Hour), Nodes: []model.Node{{IP: "8.8.8.8", Speed: 50, Carrier: model.CarrierCM}}}
	targets := Build(data, &cache, map[string][]string{}, 0.85, 3, 72*time.Hour, now)
	if targets[model.CarrierCT].Mode != ModeRealtime {
		t.Fatalf("ct mode = %s", targets[model.CarrierCT].Mode)
	}
	if targets[model.CarrierCM].Mode != ModeCache || targets[model.CarrierCM].Nodes[0].IP != "8.8.8.8" {
		t.Fatalf("cm target = %#v", targets[model.CarrierCM])
	}
	if targets[model.CarrierCU].Mode != ModeBorrowed || targets[model.CarrierCU].Donor != model.CarrierCT || targets[model.CarrierCU].Nodes[0].IP != "1.1.1.1" {
		t.Fatalf("cu target = %#v", targets[model.CarrierCU])
	}
}

func TestBuildPreservesExistingBeforeCrossCarrierBorrow(t *testing.T) {
	now := time.Now()
	data := model.SourceData{FetchedAt: now, Carriers: map[string]model.Snapshot{
		model.CarrierCT: {Carrier: model.CarrierCT, Found: true, Nodes: []model.Node{{IP: "1.1.1.1", Speed: 100}}},
	}}
	cache := state.Empty()
	targets := Build(data, &cache, map[string][]string{model.CarrierCU: {"9.9.9.9"}}, 0.85, 3, 72*time.Hour, now)
	if targets[model.CarrierCU].Mode != ModeRetained || targets[model.CarrierCU].Nodes[0].IP != "9.9.9.9" {
		t.Fatalf("unexpected cu target: %#v", targets[model.CarrierCU])
	}
}
