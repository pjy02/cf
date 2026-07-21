package syncer

import (
	"context"
	"testing"

	"github.com/pjy02/cf/internal/cloudflare"
	"github.com/pjy02/cf/internal/model"
)

type fakeDNS struct{ operations []string }

func (f *fakeDNS) CreateARecord(_ context.Context, _, _, ip string, _ int) (cloudflare.Record, error) {
	f.operations = append(f.operations, "create:"+ip)
	return cloudflare.Record{ID: "new", Content: ip}, nil
}

func (f *fakeDNS) UpdateARecord(_ context.Context, _, id, _, ip string, _ int) (cloudflare.Record, error) {
	f.operations = append(f.operations, "update:"+id+":"+ip)
	return cloudflare.Record{ID: id, Content: ip}, nil
}

func (f *fakeDNS) DeleteRecord(_ context.Context, _, id string) error {
	f.operations = append(f.operations, "delete:"+id)
	return nil
}

func TestReconcileCreatesBeforeDeleting(t *testing.T) {
	fake := &fakeDNS{}
	current := []cloudflare.Record{{ID: "old", Content: "1.1.1.1"}}
	desired := []model.Node{{IP: "8.8.8.8"}}
	report, err := Reconcile(context.Background(), fake, "zone", "ctcc.example.com", current, desired, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Created != 1 || report.Deleted != 1 || len(fake.operations) != 2 {
		t.Fatalf("unexpected report/operations: %#v %#v", report, fake.operations)
	}
	if fake.operations[0] != "create:8.8.8.8" || fake.operations[1] != "delete:old" {
		t.Fatalf("unsafe operation order: %#v", fake.operations)
	}
}

func TestReconcileNeverDeletesWhenDesiredIsEmpty(t *testing.T) {
	fake := &fakeDNS{}
	_, err := Reconcile(context.Background(), fake, "zone", "ctcc.example.com", []cloudflare.Record{{ID: "old", Content: "1.1.1.1"}}, nil, 60, false)
	if err != nil || len(fake.operations) != 0 {
		t.Fatalf("empty target changed DNS: %v %#v", err, fake.operations)
	}
}

func TestReconcileDisablesProxyOnExistingRecord(t *testing.T) {
	fake := &fakeDNS{}
	current := []cloudflare.Record{{ID: "orange", Content: "1.1.1.1", TTL: 1, Proxied: true}}
	report, err := Reconcile(context.Background(), fake, "zone", "ctcc.example.com", current, []model.Node{{IP: "1.1.1.1"}}, 60, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || len(fake.operations) != 1 || fake.operations[0] != "update:orange:1.1.1.1" {
		t.Fatalf("proxy was not disabled: %#v %#v", report, fake.operations)
	}
}
