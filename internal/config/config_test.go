package config

import "testing"

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
