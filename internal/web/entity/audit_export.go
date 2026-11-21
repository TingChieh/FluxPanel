package entity

// AuditExport is one panel's own access-audit aggregates for a date window,
// pulled by its master to merge node traffic into the master's audit.
type AuditExport struct {
	TimeLocation string             `json:"timeLocation" example:"Asia/Shanghai"`
	UTCOffset    int                `json:"utcOffset" example:"28800"`
	From         string             `json:"from" example:"2026-11-18"`
	To           string             `json:"to" example:"2026-11-19"`
	Daily        []AuditExportDaily `json:"daily"`
	IPs          []AuditExportIP    `json:"ips"`
}

// AuditExportDaily is one client/destination daily row, unclassified so the
// importing master scores it with its own rules.
type AuditExportDaily struct {
	Date       string `json:"date" example:"2026-11-19"`
	Email      string `json:"email" example:"alice"`
	RootDomain string `json:"rootDomain" example:"github.com"`
	Domain     string `json:"domain" example:"api.github.com"`
	Port       int    `json:"port" example:"443"`
	Outbound   string `json:"outbound" example:"direct"`
	HitCount   int64  `json:"hitCount" example:"120"`
	FirstSeen  int64  `json:"firstSeen" example:"1763510000"`
	LastSeen   int64  `json:"lastSeen" example:"1763540000"`
}

// AuditExportIP is one client/source-IP daily row with its activity bitmaps.
type AuditExportIP struct {
	Date        string `json:"date" example:"2026-11-19"`
	Email       string `json:"email" example:"alice"`
	SrcIP       string `json:"srcIp" example:"198.51.100.10"`
	Subnet      string `json:"subnet" example:"198.51.100.0/24"`
	HitCount    int64  `json:"hitCount" example:"300"`
	FirstSeen   int64  `json:"firstSeen" example:"1763510000"`
	LastSeen    int64  `json:"lastSeen" example:"1763540000"`
	ActiveHours uint32 `json:"activeHours" example:"1536"`
	ActiveSlots string `json:"activeSlots,omitempty" example:""`
}
