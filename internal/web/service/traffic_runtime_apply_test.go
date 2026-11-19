package service

import (
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/runtime"
	"github.com/TingChieh/FluxPanel/internal/xray"
)

func TestTrafficDisableImmediatelyUpdatesNodeRuntime(t *testing.T) {
	setupConflictDB(t)
	nodeID, fake := setupNodeRuntime(t)
	client := model.Client{Email: "spent-node", Enable: true}
	ib := nodeInbound(t, nodeID, 46301, []model.Client{client})
	if err := database.GetDB().Create(&xray.ClientTraffic{
		InboundId: ib.Id, Email: client.Email, Enable: true, Up: 100, Total: 100,
	}).Error; err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	if _, _, err := (&InboundService{}).AddTraffic(nil, nil); err != nil {
		t.Fatalf("AddTraffic: %v", err)
	}
	if got := fake.updateInbound.Load(); got != 1 {
		t.Fatalf("remote UpdateInbound calls = %d, want 1 after commit", got)
	}
}

func TestDelDepletedClientsCleansRuntimeAfterCommit(t *testing.T) {
	setupConflictDB(t)
	mgr := runtime.NewManager(runtime.LocalDeps{APIPort: func() int { return 0 }})
	fake := &fakeNodeRuntime{}
	mgr.SetLocalRuntimeOverride(fake)
	runtime.SetManager(mgr)
	t.Cleanup(func() { runtime.SetManager(nil) })

	seedInboundConflict(t, "depleted-only", "", 46303, model.VLESS, `{"network":"tcp"}`,
		`{"clients":[{"email":"gone","enable":true}]}`)
	ib := loadInboundByTag(t, "depleted-only")
	seedClientTraffic(t, ib.Id, "gone", true)
	if err := database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "gone").
		Updates(map[string]any{"up": 100, "total": 100, "reset": 0}).Error; err != nil {
		t.Fatalf("deplete traffic: %v", err)
	}
	balancer := &model.SubBalancer{Remark: "depleted", InboundIds: []int{ib.Id}, Enabled: true}
	if err := database.GetDB().Create(balancer).Error; err != nil {
		t.Fatalf("create balancer: %v", err)
	}

	if err := (&InboundService{}).DelDepletedClients(-1); err != nil {
		t.Fatalf("DelDepletedClients: %v", err)
	}
	if got := fake.delInbound.Load(); got != 1 {
		t.Fatalf("runtime DelInbound calls = %d, want 1 after commit", got)
	}
	if err := database.GetDB().First(balancer, balancer.Id).Error; err != nil {
		t.Fatalf("reload balancer: %v", err)
	}
	if len(balancer.InboundIds) != 0 {
		t.Fatalf("balancer members after the inbound went = %v, want none", balancer.InboundIds)
	}
}
