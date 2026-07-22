package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pjy02/cf/internal/model"
)

func TestParseIntervalSupportsDays(t *testing.T) {
	d, err := ParseInterval("1d")
	if err != nil || d.Hours() != 24 {
		t.Fatalf("1d = %v, %v", d, err)
	}
}

func TestConfigRejectsInvalidZone(t *testing.T) {
	cfg := Default()
	cfg.Zone = "https://example.com"
	cfg.APIToken = "token"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid zone rejection")
	}
}

func TestLoadOldConfigAddsDefaultFallbackStrategies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	oldConfig := `{"zone":"example.com","zone_id":"zone","api_token":"token","max_records":3,"speed_ratio":0.85,"ttl":60,"interval":"30m","cache_max_age":"72h","prefixes":{"cm":"cmcc","cu":"cucc","ct":"ctcc"}}`
	if err := os.WriteFile(path, []byte(oldConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, carrier := range model.CarrierOrder {
		if cfg.Fallback[carrier] != FallbackAuto {
			t.Fatalf("%s fallback = %q", carrier, cfg.Fallback[carrier])
		}
	}
}

func TestConfigRejectsBorrowingItself(t *testing.T) {
	cfg := Default()
	cfg.Zone = "example.com"
	cfg.APIToken = "token"
	cfg.Fallback[model.CarrierCU] = model.CarrierCU
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected self-borrow rejection")
	}
}
