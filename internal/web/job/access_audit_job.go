package job

import (
	"context"
	"time"

	"github.com/TingChieh/FluxPanel/internal/logger"
	"github.com/TingChieh/FluxPanel/internal/web/service"
)

// AccessAuditIngestJob incrementally ingests Xray access logs into daily audit summaries.
type AccessAuditIngestJob struct {
	auditService service.AccessAuditService
}

// NewAccessAuditIngestJob creates an incremental access-log audit job.
func NewAccessAuditIngestJob() *AccessAuditIngestJob {
	return &AccessAuditIngestJob{}
}

func (j *AccessAuditIngestJob) Run() {
	if _, err := j.auditService.IngestNow(); err != nil {
		logger.Warning("access audit ingest failed:", err)
	}
}

// AccessAuditPruneJob deletes expired audit records according to RetentionDays.
type AccessAuditPruneJob struct {
	auditService service.AccessAuditService
}

// NewAccessAuditPruneJob creates a daily audit-retention pruning job.
func NewAccessAuditPruneJob() *AccessAuditPruneJob {
	return &AccessAuditPruneJob{}
}

func (j *AccessAuditPruneJob) Run() {
	cfg := j.auditService.GetConfig()
	if err := j.auditService.PruneOldData(cfg.RetentionDays); err != nil {
		logger.Warning("access audit prune failed:", err)
	}
}

// AccessAuditNodeSyncJob merges each online node's audit snapshot into this panel.
type AccessAuditNodeSyncJob struct {
	auditService service.AccessAuditService
}

// NewAccessAuditNodeSyncJob creates the periodic node audit pull job.
func NewAccessAuditNodeSyncJob() *AccessAuditNodeSyncJob {
	return &AccessAuditNodeSyncJob{}
}

func (j *AccessAuditNodeSyncJob) Run() {
	j.auditService.PullNodeAudits(context.Background())
}

// AccessAuditDigestJob sends the daily Telegram digest once its send time passes.
type AccessAuditDigestJob struct {
	auditService service.AccessAuditService
}

// NewAccessAuditDigestJob creates the minute-level digest schedule check.
func NewAccessAuditDigestJob() *AccessAuditDigestJob {
	return &AccessAuditDigestJob{}
}

func (j *AccessAuditDigestJob) Run() {
	j.auditService.RunDigestSchedule(context.Background(), time.Now())
}
