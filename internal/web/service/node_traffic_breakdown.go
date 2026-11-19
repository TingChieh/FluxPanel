package service

import (
	"slices"
	"strings"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/xray"

	"gorm.io/gorm"
)

// NodeClientTrafficItem holds one user's cumulative traffic on a node.
type NodeClientTrafficItem struct {
	Email string `json:"email" example:"alice@example.com"`
	Up    int64  `json:"up" example:"268435456"`
	Down  int64  `json:"down" example:"1073741824"`
	Total int64  `json:"total" example:"1342177280"`
}

// NodeTrafficBreakdown holds a node's cumulative traffic and per-user breakdown.
type NodeTrafficBreakdown struct {
	NodeId       int                     `json:"nodeId" example:"1"`
	NodeName     string                  `json:"nodeName" example:"edge-node-1"`
	TotalUp      int64                   `json:"totalUp" example:"1073741824"`
	TotalDown    int64                   `json:"totalDown" example:"5368709120"`
	TotalTraffic int64                   `json:"totalTraffic" example:"6442450944"`
	Clients      []NodeClientTrafficItem `json:"clients"`
}

// ClientNodeTrafficItem holds one user's cumulative traffic attributed to one node.
type ClientNodeTrafficItem struct {
	NodeId   int    `json:"nodeId" example:"1"`
	NodeName string `json:"nodeName" example:"edge-node-1"`
	Up       int64  `json:"up" example:"268435456"`
	Down     int64  `json:"down" example:"1073741824"`
	Total    int64  `json:"total" example:"1342177280"`
}

func (s *NodeService) populateSingleNodeTraffic(n *model.Node) {
	if n == nil || n.Id <= 0 {
		return
	}
	var sum struct {
		Up   int64
		Down int64
	}
	if err := database.GetDB().Table("inbounds").
		Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
		Where("node_id = ?", n.Id).
		Scan(&sum).Error; err != nil {
		return
	}
	n.TotalUp = sum.Up
	n.TotalDown = sum.Down
	n.TotalTraffic = sum.Up + sum.Down
}

// GetNodeTrafficBreakdown returns cumulative inbound traffic and per-client usage for nodeID (0 = local panel).
func (s *NodeService) GetNodeTrafficBreakdown(nodeID int) (*NodeTrafficBreakdown, error) {
	if nodeID < 0 {
		return nil, gorm.ErrRecordNotFound
	}
	db := database.GetDB()
	if nodeID == 0 {
		return localNodeTrafficBreakdown(db)
	}

	var node model.Node
	if err := db.Model(&model.Node{}).Where("id = ?", nodeID).First(&node).Error; err != nil {
		return nil, err
	}

	var sum struct {
		Up   int64
		Down int64
	}
	if err := db.Table("inbounds").
		Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
		Where("node_id = ?", nodeID).
		Scan(&sum).Error; err != nil {
		return nil, err
	}

	var rows []model.NodeClientTraffic
	if err := db.Model(&model.NodeClientTraffic{}).Where("node_id = ?", nodeID).Find(&rows).Error; err != nil {
		return nil, err
	}
	byEmail := make(map[string]NodeClientTrafficItem, len(rows))
	for _, r := range rows {
		if r.Detached && r.AccUp == 0 && r.AccDown == 0 {
			continue
		}
		byEmail[r.Email] = NodeClientTrafficItem{
			Email: r.Email,
			Up:    r.AccUp,
			Down:  r.AccDown,
			Total: r.AccUp + r.AccDown,
		}
	}

	var attachedEmails []string
	if err := db.Table("clients").
		Distinct("clients.email").
		Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Joins("JOIN inbounds ON inbounds.id = client_inbounds.inbound_id").
		Where("inbounds.node_id = ?", nodeID).
		Pluck("clients.email", &attachedEmails).Error; err != nil {
		return nil, err
	}
	for _, email := range attachedEmails {
		if _, exists := byEmail[email]; !exists {
			byEmail[email] = NodeClientTrafficItem{Email: email}
		}
	}

	return &NodeTrafficBreakdown{
		NodeId:       node.Id,
		NodeName:     node.Name,
		TotalUp:      sum.Up,
		TotalDown:    sum.Down,
		TotalTraffic: sum.Up + sum.Down,
		Clients:      sortNodeClients(byEmail),
	}, nil
}

func localNodeTrafficBreakdown(db *gorm.DB) (*NodeTrafficBreakdown, error) {
	var sum struct {
		Up   int64
		Down int64
	}
	if err := db.Table("inbounds").
		Select("COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
		Where("node_id IS NULL").
		Scan(&sum).Error; err != nil {
		return nil, err
	}

	var localEmails []string
	if err := db.Table("clients").
		Distinct("clients.email").
		Joins("JOIN client_inbounds ON client_inbounds.client_id = clients.id").
		Joins("JOIN inbounds ON inbounds.id = client_inbounds.inbound_id").
		Where("inbounds.node_id IS NULL").
		Pluck("clients.email", &localEmails).Error; err != nil {
		return nil, err
	}
	attachedLocal := make(map[string]struct{}, len(localEmails))
	for _, email := range localEmails {
		attachedLocal[email] = struct{}{}
	}

	var totals []xray.ClientTraffic
	if err := db.Model(&xray.ClientTraffic{}).Find(&totals).Error; err != nil {
		return nil, err
	}
	var remoteSums []struct {
		Email string
		Up    int64
		Down  int64
	}
	if err := db.Table("node_client_traffics").
		Select("email, COALESCE(SUM(acc_up), 0) AS up, COALESCE(SUM(acc_down), 0) AS down").
		Group("email").
		Scan(&remoteSums).Error; err != nil {
		return nil, err
	}
	remoteByEmail := make(map[string][2]int64, len(remoteSums))
	for _, rs := range remoteSums {
		remoteByEmail[rs.Email] = [2]int64{rs.Up, rs.Down}
	}

	byEmail := make(map[string]NodeClientTrafficItem, len(localEmails))
	for _, ct := range totals {
		rem := remoteByEmail[ct.Email]
		up := max(int64(0), ct.Up-rem[0])
		down := max(int64(0), ct.Down-rem[1])
		_, isAttached := attachedLocal[ct.Email]
		if !isAttached && up == 0 && down == 0 {
			continue
		}
		byEmail[ct.Email] = NodeClientTrafficItem{
			Email: ct.Email,
			Up:    up,
			Down:  down,
			Total: up + down,
		}
	}
	for _, email := range localEmails {
		if _, exists := byEmail[email]; !exists {
			byEmail[email] = NodeClientTrafficItem{Email: email}
		}
	}

	return &NodeTrafficBreakdown{
		NodeId:       0,
		NodeName:     "",
		TotalUp:      sum.Up,
		TotalDown:    sum.Down,
		TotalTraffic: sum.Up + sum.Down,
		Clients:      sortNodeClients(byEmail),
	}, nil
}

func sortNodeClients(byEmail map[string]NodeClientTrafficItem) []NodeClientTrafficItem {
	out := make([]NodeClientTrafficItem, 0, len(byEmail))
	for _, item := range byEmail {
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b NodeClientTrafficItem) int {
		if a.Total != b.Total {
			if a.Total > b.Total {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Email, b.Email)
	})
	return out
}

// GetClientNodeTraffics splits one user's cumulative traffic across local and remote nodes.
func (s *InboundService) GetClientNodeTraffics(email string) ([]ClientNodeTrafficItem, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, gorm.ErrRecordNotFound
	}
	db := database.GetDB()

	var ct xray.ClientTraffic
	hasTotal := db.Model(&xray.ClientTraffic{}).Where("email = ?", email).First(&ct).Error == nil

	var nodeRows []struct {
		NodeId   int
		NodeName string
		AccUp    int64
		AccDown  int64
		Detached bool
	}
	if err := db.Table("node_client_traffics nct").
		Select("nct.node_id AS node_id, COALESCE(nodes.name, '') AS node_name, nct.acc_up AS acc_up, nct.acc_down AS acc_down, nct.detached AS detached").
		Joins("LEFT JOIN nodes ON nodes.id = nct.node_id").
		Where("nct.email = ?", email).
		Scan(&nodeRows).Error; err != nil {
		return nil, err
	}

	var attached []struct {
		NodeId   *int
		NodeName string
	}
	if err := db.Table("inbounds").
		Select("DISTINCT inbounds.node_id AS node_id, COALESCE(nodes.name, '') AS node_name").
		Joins("JOIN client_inbounds ON client_inbounds.inbound_id = inbounds.id").
		Joins("JOIN clients ON clients.id = client_inbounds.client_id").
		Joins("LEFT JOIN nodes ON nodes.id = inbounds.node_id").
		Where("clients.email = ?", email).
		Scan(&attached).Error; err != nil {
		return nil, err
	}

	attachedLocal := false
	byNode := make(map[int]ClientNodeTrafficItem)
	for _, a := range attached {
		if a.NodeId == nil {
			attachedLocal = true
			continue
		}
		if *a.NodeId > 0 {
			byNode[*a.NodeId] = ClientNodeTrafficItem{NodeId: *a.NodeId, NodeName: a.NodeName}
		}
	}

	var sumRemoteUp, sumRemoteDown int64
	for _, nr := range nodeRows {
		if nr.Detached && nr.AccUp == 0 && nr.AccDown == 0 {
			continue
		}
		sumRemoteUp += nr.AccUp
		sumRemoteDown += nr.AccDown
		byNode[nr.NodeId] = ClientNodeTrafficItem{
			NodeId:   nr.NodeId,
			NodeName: nr.NodeName,
			Up:       nr.AccUp,
			Down:     nr.AccDown,
			Total:    nr.AccUp + nr.AccDown,
		}
	}

	var localUp, localDown int64
	if hasTotal {
		localUp = max(int64(0), ct.Up-sumRemoteUp)
		localDown = max(int64(0), ct.Down-sumRemoteDown)
	}

	out := make([]ClientNodeTrafficItem, 0, len(byNode)+1)
	if attachedLocal || localUp > 0 || localDown > 0 {
		out = append(out, ClientNodeTrafficItem{
			NodeId: 0,
			Up:     localUp,
			Down:   localDown,
			Total:  localUp + localDown,
		})
	}

	remote := make([]ClientNodeTrafficItem, 0, len(byNode))
	for _, item := range byNode {
		remote = append(remote, item)
	}
	slices.SortFunc(remote, func(a, b ClientNodeTrafficItem) int {
		return a.NodeId - b.NodeId
	})
	return append(out, remote...), nil
}
