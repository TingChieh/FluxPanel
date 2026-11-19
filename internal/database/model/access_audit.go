package model

// AccessAuditDaily aggregates per-client daily destination hits from Xray access logs.
type AccessAuditDaily struct {
	Id         uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Date       string `json:"date" gorm:"size:10;not null;uniqueIndex:idx_audit_daily_key;index"`
	Email      string `json:"email" gorm:"size:255;not null;uniqueIndex:idx_audit_daily_key;index"`
	RootDomain string `json:"rootDomain" gorm:"size:255;not null;index"`
	Domain     string `json:"domain" gorm:"size:255;not null;uniqueIndex:idx_audit_daily_key"`
	Port       int    `json:"port" gorm:"not null;uniqueIndex:idx_audit_daily_key"`
	Outbound   string `json:"outbound" gorm:"size:128"`
	Category   string `json:"category" gorm:"size:32;index"`
	RiskLevel  int    `json:"riskLevel" gorm:"not null;default:0;index"`
	RuleHit    string `json:"ruleHit" gorm:"size:255"`
	HitCount   int64  `json:"hitCount" gorm:"not null;default:0"`
	FirstSeen  int64  `json:"firstSeen" gorm:"not null;default:0"`
	LastSeen   int64  `json:"lastSeen" gorm:"not null;default:0"`
}

// AccessAuditClientIP tracks per-client daily source IPs and hourly activity bitmasks.
type AccessAuditClientIP struct {
	Id          uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Date        string `json:"date" gorm:"size:10;not null;uniqueIndex:idx_audit_ip_key;index"`
	Email       string `json:"email" gorm:"size:255;not null;uniqueIndex:idx_audit_ip_key;index"`
	SrcIP       string `json:"srcIp" gorm:"size:64;not null;uniqueIndex:idx_audit_ip_key"`
	Subnet      string `json:"subnet" gorm:"size:64;not null;index"`
	HitCount    int64  `json:"hitCount" gorm:"not null;default:0"`
	FirstSeen   int64  `json:"firstSeen" gorm:"not null;default:0"`
	LastSeen    int64  `json:"lastSeen" gorm:"not null;default:0"`
	ActiveHours uint32 `json:"activeHours" gorm:"not null;default:0"`
}

// AccessAuditAlert records deduplicated sensitive-content and account-sharing alerts.
type AccessAuditAlert struct {
	Id        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt int64  `json:"createdAt" gorm:"not null;index"`
	Date      string `json:"date" gorm:"size:10;not null;uniqueIndex:idx_audit_alert_key;index"`
	Email     string `json:"email" gorm:"size:255;not null;uniqueIndex:idx_audit_alert_key;index"`
	AlertType string `json:"alertType" gorm:"size:32;not null;uniqueIndex:idx_audit_alert_key;index"`
	Severity  int    `json:"severity" gorm:"not null;default:1;index"`
	RuleName  string `json:"ruleName" gorm:"size:128;not null;uniqueIndex:idx_audit_alert_key"`
	Target    string `json:"target" gorm:"size:255;not null;uniqueIndex:idx_audit_alert_key"`
	SrcIP     string `json:"srcIp" gorm:"size:64"`
	HitCount  int64  `json:"hitCount" gorm:"not null;default:1"`
	Detail    string `json:"detail" gorm:"size:512"`
	Acked     bool   `json:"acked" gorm:"not null;default:false;index"`
}
