package service

import (
	"strings"
	"testing"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
)

// A node-managed inbound arrives by adoption, so its protocol can be one the
// master never assigns itself. Editing its share metadata must still work.
func TestUpdateInbound_RejectsProtocolChangeToNodeIneligible(t *testing.T) {
	setupConflictDB(t)
	nodeID := 6
	seedNodeRow(t, database.GetDB(), &model.Node{Id: nodeID, Name: "n6", Address: "127.0.0.1", Port: 2096, ApiToken: "tok", Enable: true, PanelVersion: "v3.4.1"})
	seedInboundConflictNode(t, "vless-node", "127.0.0.1", 4064, model.VLESS,
		`{"network":"tcp","security":"none"}`,
		`{"clients":[{"id":"11111111-2222-4333-8444-555555555555","email":"vn-c","enable":true}],"decryption":"none"}`, &nodeID)

	var existing model.Inbound
	if err := database.GetDB().Where("tag = ?", "vless-node").First(&existing).Error; err != nil {
		t.Fatalf("read seeded row: %v", err)
	}

	cases := []struct {
		name     string
		protocol model.Protocol
		settings string
		wantErr  string
	}{
		{"panel-local protocol", model.HTTP, `{"accounts":[]}`, "cannot be assigned to a node"},
		{
			"protocol newer than the node", model.MASQUE,
			`{"clients":[{"email":"vn-c","enable":true,"secret":"ee0123456789abcdef0123456789abcdef"}]}`, "need v3.9.1 or newer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			update := existing
			update.Protocol = tc.protocol
			update.Settings = tc.settings
			if _, _, err := (&InboundService{}).UpdateInbound(&update); err == nil ||
				!strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, tc.wantErr)
			}
		})
	}
}
