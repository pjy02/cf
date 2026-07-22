package fallback

import (
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/model"
	"github.com/pjy02/cf/internal/selector"
	"github.com/pjy02/cf/internal/state"
)

const (
	ModeRealtime = "实时"
	ModeCache    = "缓存"
	ModeStale    = "过期缓存"
	ModeRetained = "保留现有记录"
	ModeBorrowed = "降级借用"
	ModeMissing  = "无可用数据"
)

type Target struct {
	Nodes  []model.Node
	Mode   string
	Detail string
	Donor  string
}

func Build(data model.SourceData, cache *state.State, existing map[string][]string, strategies map[string]string, ratio float64, limit int, cacheMaxAge time.Duration, now time.Time) map[string]Target {
	current := make(map[string][]model.Node)
	for _, carrier := range model.CarrierOrder {
		snapshot, ok := data.Carriers[carrier]
		if !ok || !snapshot.Found {
			continue
		}
		selected := selector.Select(snapshot.Nodes, ratio, limit)
		if len(selected) == 0 {
			continue
		}
		current[carrier] = selected
		cache.Carriers[carrier] = state.CacheEntry{SourceTime: snapshot.SourceTime, FetchedAt: data.FetchedAt, Nodes: snapshot.Nodes}
	}

	targets := make(map[string]Target)
	for _, carrier := range model.CarrierOrder {
		if nodes := current[carrier]; len(nodes) > 0 {
			targets[carrier] = Target{Nodes: nodes, Mode: ModeRealtime, Detail: sourceDetail(data.Carriers[carrier].SourceTime)}
			continue
		}

		strategy := strategies[carrier]
		if strategy == "" {
			strategy = config.FallbackAuto
		}
		if donor, nodes := currentForStrategy(data, current, carrier, strategy); len(nodes) > 0 {
			detail := fmt.Sprintf("临时借用%s本轮网页结果（策略：%s）", model.CarrierNames[donor], strategyLabel(strategy))
			targets[carrier] = Target{Nodes: nodes, Mode: ModeBorrowed, Donor: donor, Detail: detail}
			continue
		}

		if entry, ok := cache.Carriers[carrier]; ok {
			nodes := selector.Select(entry.Nodes, ratio, limit)
			if len(nodes) > 0 {
				age := now.Sub(entry.FetchedAt)
				mode := ModeCache
				if cacheMaxAge > 0 && age > cacheMaxAge {
					mode = ModeStale
				}
				targets[carrier] = Target{Nodes: nodes, Mode: mode, Detail: fmt.Sprintf("网页缓存于 %s", formatTime(entry.FetchedAt))}
				continue
			}
		}
		if donor, nodes := cacheForStrategy(cache, carrier, strategy, ratio, limit); len(nodes) > 0 {
			targets[carrier] = Target{Nodes: nodes, Mode: ModeBorrowed, Donor: donor, Detail: fmt.Sprintf("临时借用%s历史网页结果（策略：%s）", model.CarrierNames[donor], strategyLabel(strategy))}
			continue
		}
		if nodes := existingNodes(existing[carrier], carrier, limit); len(nodes) > 0 {
			targets[carrier] = Target{Nodes: nodes, Mode: ModeRetained, Detail: "没有可用网页结果，保留 Cloudflare 当前有效 A 记录"}
			continue
		}
		targets[carrier] = Target{Mode: ModeMissing, Detail: "没有任何可靠 IP，跳过该域名修改"}
	}
	return targets
}

func currentForStrategy(data model.SourceData, current map[string][]model.Node, carrier, strategy string) (string, []model.Node) {
	switch strategy {
	case config.FallbackOff:
		return "", nil
	case config.FallbackAuto:
		return newestCurrent(data, current, carrier)
	default:
		if strategy == carrier {
			return "", nil
		}
		return strategy, cloneForCarrier(current[strategy], carrier)
	}
}

func cacheForStrategy(cache *state.State, carrier, strategy string, ratio float64, limit int) (string, []model.Node) {
	switch strategy {
	case config.FallbackOff:
		return "", nil
	case config.FallbackAuto:
		return newestCache(cache, carrier, ratio, limit)
	default:
		if strategy == carrier {
			return "", nil
		}
		entry, ok := cache.Carriers[strategy]
		if !ok {
			return "", nil
		}
		return strategy, cloneForCarrier(selector.Select(entry.Nodes, ratio, limit), carrier)
	}
}

func strategyLabel(strategy string) string {
	switch strategy {
	case config.FallbackAuto:
		return "自动选择"
	case config.FallbackOff:
		return "禁止借用"
	default:
		return "固定" + model.CarrierNames[strategy]
	}
}

func existingNodes(ips []string, carrier string, limit int) []model.Node {
	var nodes []model.Node
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		if err != nil || !addr.Is4() {
			continue
		}
		nodes = append(nodes, model.Node{IP: addr.String(), Speed: 1, Carrier: carrier})
	}
	return selector.Select(nodes, 0, limit)
}

func newestCurrent(data model.SourceData, current map[string][]model.Node, excluded string) (string, []model.Node) {
	type candidate struct {
		carrier string
		time    time.Time
		nodes   []model.Node
	}
	var candidates []candidate
	for carrier, nodes := range current {
		if carrier == excluded || len(nodes) == 0 {
			continue
		}
		t := parseSourceTime(data.Carriers[carrier].SourceTime)
		if t.IsZero() {
			t = data.FetchedAt
		}
		candidates = append(candidates, candidate{carrier: carrier, time: t, nodes: nodes})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].time.Equal(candidates[j].time) {
			return candidates[i].time.After(candidates[j].time)
		}
		if candidates[i].nodes[0].Speed != candidates[j].nodes[0].Speed {
			return candidates[i].nodes[0].Speed > candidates[j].nodes[0].Speed
		}
		return candidates[i].carrier < candidates[j].carrier
	})
	if len(candidates) == 0 {
		return "", nil
	}
	return candidates[0].carrier, cloneForCarrier(candidates[0].nodes, excluded)
}

func newestCache(cache *state.State, excluded string, ratio float64, limit int) (string, []model.Node) {
	type candidate struct {
		carrier string
		time    time.Time
		nodes   []model.Node
	}
	var candidates []candidate
	for carrier, entry := range cache.Carriers {
		if carrier == excluded {
			continue
		}
		nodes := selector.Select(entry.Nodes, ratio, limit)
		if len(nodes) > 0 {
			candidates = append(candidates, candidate{carrier: carrier, time: entry.FetchedAt, nodes: nodes})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].time.Equal(candidates[j].time) {
			return candidates[i].time.After(candidates[j].time)
		}
		if candidates[i].nodes[0].Speed != candidates[j].nodes[0].Speed {
			return candidates[i].nodes[0].Speed > candidates[j].nodes[0].Speed
		}
		return candidates[i].carrier < candidates[j].carrier
	})
	if len(candidates) == 0 {
		return "", nil
	}
	return candidates[0].carrier, cloneForCarrier(candidates[0].nodes, excluded)
}

func cloneForCarrier(nodes []model.Node, carrier string) []model.Node {
	result := make([]model.Node, len(nodes))
	copy(result, nodes)
	for i := range result {
		result[i].Carrier = carrier
	}
	return result
}

func parseSourceTime(value string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", value, time.Local)
	return t
}

func sourceDetail(value string) string {
	if value == "" {
		return "本轮网页数据"
	}
	return "网页同步时间 " + value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "未知时间"
	}
	return value.Format("2006-01-02 15:04")
}
