package controller

import (
	"strconv"

	"github.com/TingChieh/FluxPanel/internal/web/service"

	"github.com/gin-gonic/gin"
)

// AuditController exposes access-log audit summaries, client detail views, alerts, and rules.
type AuditController struct {
	auditService service.AccessAuditService
}

// NewAuditController wires the /panel/api/audit routes onto g.
func NewAuditController(g *gin.RouterGroup) *AuditController {
	a := &AuditController{}
	a.initRouter(g)
	return a
}

func (a *AuditController) initRouter(g *gin.RouterGroup) {
	g.GET("/summary", a.summary)
	g.GET("/client/:email", a.clientDetail)
	g.GET("/alerts", a.alerts)
	g.POST("/alerts/ack/:id", a.ackAlert)
	g.POST("/alerts/ackAll", a.ackAllAlerts)
	g.GET("/config", a.getConfig)
	g.POST("/config", a.saveConfig)
	g.POST("/ingest", a.ingest)
}

func (a *AuditController) summary(c *gin.Context) {
	dateStr := c.Query("date")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "1"))
	res, err := a.auditService.GetSummary(dateStr, days)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, res, nil)
}

func (a *AuditController) clientDetail(c *gin.Context) {
	email := c.Param("email")
	dateStr := c.Query("date")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "1"))
	hideNoise := c.DefaultQuery("hideNoise", "true") != "false"
	res, err := a.auditService.GetClientDetail(email, dateStr, days, hideNoise)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, res, nil)
}

func (a *AuditController) alerts(c *gin.Context) {
	dateStr := c.Query("date")
	email := c.Query("email")
	onlyUnacked := c.Query("onlyUnacked") == "true"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	res, err := a.auditService.GetAlerts(dateStr, email, onlyUnacked, limit)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, res, nil)
}

func (a *AuditController) ackAlert(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err = a.auditService.AckAlert(uint(id))
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.ack"), err)
}

func (a *AuditController) ackAllAlerts(c *gin.Context) {
	err := a.auditService.AckAllAlerts()
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.ackAll"), err)
}

func (a *AuditController) getConfig(c *gin.Context) {
	jsonObj(c, a.auditService.GetConfig(), nil)
}

func (a *AuditController) saveConfig(c *gin.Context) {
	var req service.AuditConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err := a.auditService.SaveConfig(req.Config, req.Reevaluate)
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.saveConfig"), err)
}

func (a *AuditController) ingest(c *gin.Context) {
	res, err := a.auditService.IngestNow()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, res, nil)
}
