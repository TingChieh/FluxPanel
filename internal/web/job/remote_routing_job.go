package job

import (
	"github.com/TingChieh/FluxPanel/internal/logger"
	"github.com/TingChieh/FluxPanel/internal/sub"
	"github.com/TingChieh/FluxPanel/internal/web/service"
)

// RemoteRoutingJob keeps remote Clash/Mihomo and JSON routing URLs warm: all
// network work runs here (cron + startup warm), never in a request handler.
type RemoteRoutingJob struct {
	settingService service.SettingService
}

func NewRemoteRoutingJob() *RemoteRoutingJob {
	return &RemoteRoutingJob{}
}

func (j *RemoteRoutingJob) Run() {
	clash, err := j.settingService.GetSubClashRules()
	if err != nil {
		logger.Warning("Could not read Clash routing source:", err)
		return
	}
	jsonRouting, err := j.settingService.GetSubJsonRoutingRules()
	if err != nil {
		logger.Warning("Could not read JSON subscription routing source:", err)
		return
	}
	sub.RefreshRemoteRoutingSources(clash, jsonRouting)
}
