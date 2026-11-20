package model

// TrafficHourly stores per-hour incremental traffic for a node and client email.
// NodeId 0 is the local panel; empty Email stores unattributed inbound traffic.
type TrafficHourly struct {
	Id         uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	HourBucket int64  `json:"hourBucket" gorm:"not null;uniqueIndex:idx_traffic_hourly_key,priority:1;index:idx_traffic_hourly_node,priority:2;index:idx_traffic_hourly_email,priority:2"`
	NodeId     int    `json:"nodeId" gorm:"not null;default:0;uniqueIndex:idx_traffic_hourly_key,priority:2;index:idx_traffic_hourly_node,priority:1"`
	Email      string `json:"email" gorm:"size:255;not null;default:'';uniqueIndex:idx_traffic_hourly_key,priority:3;index:idx_traffic_hourly_email,priority:1"`
	Up         int64  `json:"up" gorm:"not null;default:0"`
	Down       int64  `json:"down" gorm:"not null;default:0"`
}

// TrafficDaily archives rolled-up daily traffic in the panel timezone beyond hourly retention.
type TrafficDaily struct {
	Id     uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Date   string `json:"date" gorm:"size:10;not null;uniqueIndex:idx_traffic_daily_key,priority:1;index:idx_traffic_daily_node,priority:2;index:idx_traffic_daily_email,priority:2"`
	NodeId int    `json:"nodeId" gorm:"not null;default:0;uniqueIndex:idx_traffic_daily_key,priority:2;index:idx_traffic_daily_node,priority:1"`
	Email  string `json:"email" gorm:"size:255;not null;default:'';uniqueIndex:idx_traffic_daily_key,priority:3;index:idx_traffic_daily_email,priority:1"`
	Up     int64  `json:"up" gorm:"not null;default:0"`
	Down   int64  `json:"down" gorm:"not null;default:0"`
}
