package selector

import (
	"net/netip"
	"sort"

	"github.com/pjy02/cf/internal/model"
)

func Select(nodes []model.Node, ratio float64, limit int) []model.Node {
	valid := make([]model.Node, 0, len(nodes))
	seen := make(map[string]bool)
	for _, node := range nodes {
		addr, err := netip.ParseAddr(node.IP)
		if err != nil || !addr.Is4() || !isPublic(addr) || node.Speed <= 0 || seen[addr.String()] {
			continue
		}
		node.IP = addr.String()
		seen[node.IP] = true
		valid = append(valid, node)
	}
	if len(valid) == 0 || limit <= 0 {
		return nil
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if valid[i].Speed != valid[j].Speed {
			return valid[i].Speed > valid[j].Speed
		}
		if valid[i].Latency != valid[j].Latency {
			return valid[i].Latency < valid[j].Latency
		}
		return valid[i].IP < valid[j].IP
	})
	threshold := valid[0].Speed * ratio
	selected := make([]model.Node, 0, limit)
	for _, node := range valid {
		if node.Speed+1e-9 < threshold {
			continue
		}
		selected = append(selected, node)
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func isPublic(addr netip.Addr) bool {
	return !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsMulticast() &&
		!addr.IsUnspecified() && !addr.IsLinkLocalUnicast()
}
