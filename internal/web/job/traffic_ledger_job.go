package job

import (
	"time"

	"github.com/TingChieh/FluxPanel/internal/logger"
	"github.com/TingChieh/FluxPanel/internal/web/service"
)

// TrafficFlushJob periodically flushes buffered hourly traffic deltas to SQLite.
type TrafficFlushJob struct{}

func NewTrafficFlushJob() *TrafficFlushJob {
	return &TrafficFlushJob{}
}

func (j *TrafficFlushJob) Run() {
	if err := service.FlushTrafficLedger(); err != nil {
		logger.Warning("flush traffic ledger failed:", err)
	}
}

// TrafficRollupJob archives hourly rows older than 90 days into daily rows and prunes >365d dailies.
type TrafficRollupJob struct {
	loc *time.Location
}

func NewTrafficRollupJob(loc *time.Location) *TrafficRollupJob {
	return &TrafficRollupJob{loc: loc}
}

func (j *TrafficRollupJob) Run() {
	if err := service.RollupAndPruneTrafficHistory(time.Now(), j.loc); err != nil {
		logger.Warning("rollup traffic history failed:", err)
	}
}
