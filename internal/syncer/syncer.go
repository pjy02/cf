package syncer

import (
	"context"
	"fmt"

	"github.com/pjy02/cf/internal/cloudflare"
	"github.com/pjy02/cf/internal/model"
)

type DNSClient interface {
	CreateARecord(context.Context, string, string, string, int) (cloudflare.Record, error)
	UpdateARecord(context.Context, string, string, string, string, int) (cloudflare.Record, error)
	DeleteRecord(context.Context, string, string) error
}

type Report struct {
	Hostname  string
	Desired   []string
	Created   int
	Updated   int
	Deleted   int
	Unchanged int
	DryRun    bool
}

func Reconcile(ctx context.Context, client DNSClient, zoneID, hostname string, current []cloudflare.Record, desiredNodes []model.Node, ttl int, dryRun bool) (Report, error) {
	report := Report{Hostname: hostname, DryRun: dryRun}
	desired := make(map[string]bool)
	for _, node := range desiredNodes {
		if !desired[node.IP] {
			desired[node.IP] = true
			report.Desired = append(report.Desired, node.IP)
		}
	}
	if len(desired) == 0 {
		return report, nil
	}

	existingByIP := make(map[string][]cloudflare.Record)
	for _, record := range current {
		existingByIP[record.Content] = append(existingByIP[record.Content], record)
	}
	for ip := range desired {
		if records := existingByIP[ip]; len(records) > 0 {
			if records[0].Proxied || records[0].TTL != ttl {
				if dryRun {
					report.Updated++
					continue
				}
				if _, err := client.UpdateARecord(ctx, zoneID, records[0].ID, hostname, ip, ttl); err != nil {
					return report, err
				}
				report.Updated++
			} else {
				report.Unchanged++
			}
			continue
		}
		if dryRun {
			report.Created++
			continue
		}
		if _, err := client.CreateARecord(ctx, zoneID, hostname, ip, ttl); err != nil {
			return report, err
		}
		report.Created++
	}

	for ip, records := range existingByIP {
		keep := 0
		if desired[ip] {
			keep = 1
		}
		for i := keep; i < len(records); i++ {
			if dryRun {
				report.Deleted++
				continue
			}
			if err := client.DeleteRecord(ctx, zoneID, records[i].ID); err != nil {
				return report, fmt.Errorf("清理 %s 的旧记录: %w", hostname, err)
			}
			report.Deleted++
		}
	}
	return report, nil
}
