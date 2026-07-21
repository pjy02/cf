package selector

import (
	"testing"

	"github.com/pjy02/cf/internal/model"
)

func TestSelectUsesFastestRatioAndLimit(t *testing.T) {
	nodes := []model.Node{
		{IP: "108.162.198.177", Speed: 65.92, Latency: 56.59},
		{IP: "108.162.192.211", Speed: 61.90, Latency: 62.44},
		{IP: "162.159.38.214", Speed: 60.31, Latency: 56.20},
		{IP: "172.64.41.101", Speed: 59.49, Latency: 66.57},
		{IP: "172.64.53.213", Speed: 50.25, Latency: 58.84},
	}
	selected := Select(nodes, 0.85, 3)
	if len(selected) != 3 {
		t.Fatalf("got %d selected nodes", len(selected))
	}
	if selected[0].IP != "108.162.198.177" || selected[2].IP != "162.159.38.214" {
		t.Fatalf("unexpected order: %#v", selected)
	}
}

func TestSelectRejectsPrivateAndDuplicateIPs(t *testing.T) {
	nodes := []model.Node{{IP: "10.0.0.1", Speed: 100}, {IP: "1.1.1.1", Speed: 90}, {IP: "1.1.1.1", Speed: 80}}
	selected := Select(nodes, 0.85, 3)
	if len(selected) != 1 || selected[0].IP != "1.1.1.1" {
		t.Fatalf("unexpected selection: %#v", selected)
	}
}
