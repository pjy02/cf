package systemd

import (
	"strings"
	"testing"
)

func TestRenderServiceQuotesBinaryPath(t *testing.T) {
	unit := RenderService("/opt/cf sync/cfsync")
	if !strings.Contains(unit, `ExecStart="/opt/cf sync/cfsync" sync --quiet`) {
		t.Fatalf("binary path is not quoted: %s", unit)
	}
}

func TestRenderTimer(t *testing.T) {
	unit, err := RenderTimer("1d")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, "OnUnitActiveSec=1d") || !strings.Contains(unit, "Persistent=true") {
		t.Fatalf("unexpected timer: %s", unit)
	}
}
