package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/TingChieh/FluxPanel/internal/web/entity"
	"github.com/TingChieh/FluxPanel/internal/web/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// TrafficController exposes historical day/week/month traffic analytics and live-speed endpoints.
type TrafficController struct {
	trafficService service.TrafficAnalyticsService
}

// NewTrafficController wires the /panel/api/traffic routes onto g.
func NewTrafficController(g *gin.RouterGroup) *TrafficController {
	tc := &TrafficController{}
	tc.initRouter(g)
	return tc
}

func (tc *TrafficController) initRouter(g *gin.RouterGroup) {
	g.GET("/overview", tc.overview)
	g.GET("/clients", tc.clients)
	g.GET("/client/:email", tc.clientDetail)
	g.GET("/node/:id", tc.nodeDetail)
}

func writeTrafficError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidTrafficQuery):
		c.JSON(http.StatusBadRequest, entity.Msg{Success: false, Msg: err.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, entity.Msg{Success: false, Msg: "node not found"})
	default:
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
	}
}

func parsePageParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "25"))
	if pageSize <= 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func (tc *TrafficController) overview(c *gin.Context) {
	period := c.DefaultQuery("period", "day")
	anchor := c.Query("anchor")
	res, err := tc.trafficService.GetOverview(period, anchor)
	if err != nil {
		writeTrafficError(c, err)
		return
	}
	jsonObj(c, res, nil)
}

func (tc *TrafficController) clients(c *gin.Context) {
	period := c.DefaultQuery("period", "day")
	anchor := c.Query("anchor")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort", "traffic")
	activeOnly := c.Query("activeOnly") == "true"
	page, pageSize := parsePageParams(c)

	res, err := tc.trafficService.GetClientsPage(period, anchor, search, sortBy, activeOnly, page, pageSize)
	if err != nil {
		writeTrafficError(c, err)
		return
	}
	jsonObj(c, res, nil)
}

func (tc *TrafficController) clientDetail(c *gin.Context) {
	email := c.Param("email")
	period := c.DefaultQuery("period", "day")
	anchor := c.Query("anchor")
	res, err := tc.trafficService.GetClientDetail(email, period, anchor)
	if err != nil {
		writeTrafficError(c, err)
		return
	}
	jsonObj(c, res, nil)
}

func (tc *TrafficController) nodeDetail(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil || nodeID < 0 {
		writeTrafficError(c, service.ErrInvalidTrafficQuery)
		return
	}
	period := c.DefaultQuery("period", "day")
	anchor := c.Query("anchor")
	search := c.Query("search")
	page, pageSize := parsePageParams(c)

	res, err := tc.trafficService.GetNodeDetail(nodeID, period, anchor, search, page, pageSize)
	if err != nil {
		writeTrafficError(c, err)
		return
	}
	jsonObj(c, res, nil)
}
