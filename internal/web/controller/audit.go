package controller

import (
	"fmt"
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
	g.GET("/export", a.export)
	g.GET("/telegram", a.getTelegram)
	g.POST("/telegram", a.saveTelegram)
	g.POST("/telegram/test", a.testTelegram)
	g.POST("/telegram/relay", a.relayTelegram)
	g.GET("/digest", a.getDigest)
	g.POST("/digest", a.saveDigest)
	g.GET("/digest/preview", a.previewDigest)
	g.POST("/digest/send", a.sendDigest)
}

// auditSourceParam reads the optional nodeId filter; absent or invalid means every source.
func auditSourceParam(c *gin.Context) int {
	id, err := strconv.Atoi(c.Query("nodeId"))
	if err != nil || id < service.AuditLocalSource {
		return service.AuditAllSources
	}
	return id
}

func (a *AuditController) summary(c *gin.Context) {
	dateStr := c.Query("date")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "1"))
	res, err := a.auditService.GetSummary(dateStr, days, auditSourceParam(c))
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
	res, err := a.auditService.GetClientDetail(email, dateStr, days, hideNoise, auditSourceParam(c))
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
	pulled := a.auditService.PullNodeAudits(c.Request.Context())
	res.NodesSynced, res.NodesFailed = pulled.Synced, pulled.Failed
	jsonObj(c, res, nil)
}

func (a *AuditController) export(c *gin.Context) {
	res, err := a.auditService.ExportLocal(c.Query("from"), c.Query("to"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, res, nil)
}

func (a *AuditController) getTelegram(c *gin.Context) {
	jsonObj(c, a.auditService.GetTelegramConfig(), nil)
}

func (a *AuditController) saveTelegram(c *gin.Context) {
	var req service.AuditTelegramConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.saveTelegram"), a.auditService.SaveTelegramConfig(req))
}

func (a *AuditController) testTelegram(c *gin.Context) {
	err := a.auditService.SendTelegram(c.Request.Context(), I18nWeb(c, "pages.audit.telegram.testMessage"))
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.testTelegram"), err)
}

func (a *AuditController) relayTelegram(c *gin.Context) {
	var req service.AuditTelegramRelayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if req.Text == "" || len(req.Text) > service.MaxAuditTelegramRelayBytes {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), fmt.Errorf("relay text must be 1..%d bytes", service.MaxAuditTelegramRelayBytes))
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.testTelegram"), a.auditService.SendTelegram(c.Request.Context(), req.Text))
}

func (a *AuditController) getDigest(c *gin.Context) {
	jsonObj(c, a.auditService.GetDigestConfig(), nil)
}

func (a *AuditController) saveDigest(c *gin.Context) {
	var req service.AuditDigestConfigUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.saveDigest"), a.auditService.SaveDigestConfig(req))
}

func (a *AuditController) digestDate(c *gin.Context) string {
	if d := c.Query("date"); d != "" {
		return d
	}
	return a.auditService.PreviousDay()
}

func (a *AuditController) previewDigest(c *gin.Context) {
	date := a.digestDate(c)
	text, err := a.auditService.BuildDigest(date, a.auditService.GetDigestConfig().Lang)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, service.AuditDigestPreview{Date: date, Text: text}, nil)
}

func (a *AuditController) sendDigest(c *gin.Context) {
	jsonMsg(c, I18nWeb(c, "pages.audit.toasts.sendDigest"), a.auditService.SendDigest(c.Request.Context(), a.digestDate(c)))
}
