package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/TingChieh/FluxPanel/internal/database"
	"github.com/TingChieh/FluxPanel/internal/database/dbtest"
	"github.com/TingChieh/FluxPanel/internal/database/model"
	"github.com/TingChieh/FluxPanel/internal/web/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newTrafficControllerTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	service.ResetTrafficLedgerForTest()
	service.ResetLiveSpeedsForTest()
	t.Cleanup(func() {
		service.ResetTrafficLedgerForTest()
		service.ResetLiveSpeedsForTest()
	})

	engine := gin.New()
	store := cookie.NewStore([]byte("traffic-test-secret"))
	engine.Use(sessions.Sessions("3x-ui", store))
	NewTrafficController(engine.Group("/panel/api/traffic"))
	return engine
}

func TestTrafficController_EndpointsReturnExpectedPayloads(t *testing.T) {
	engine := newTrafficControllerTestEngine(t)
	db := database.GetDB()

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	if err := db.Create(&model.ClientRecord{Email: "alice", UUID: "u1", SubID: "s1", Enable: true}).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := db.Create(&model.TrafficHourly{
		HourBucket: now.Truncate(time.Hour).Unix(),
		NodeId:     0,
		Email:      "alice",
		Up:         1200,
		Down:       3400,
	}).Error; err != nil {
		t.Fatalf("seed hourly: %v", err)
	}

	paths := []string{
		"/panel/api/traffic/overview?period=day&anchor=" + today,
		"/panel/api/traffic/clients?period=day&anchor=" + today + "&page=1&pageSize=200&sort=traffic&activeOnly=true",
		"/panel/api/traffic/client/alice?period=day&anchor=" + today,
		"/panel/api/traffic/node/0?period=day&anchor=" + today,
	}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body=%s", p, rec.Code, rec.Body.String())
		}
		var env struct {
			Success bool            `json:"success"`
			Obj     json.RawMessage `json:"obj"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal %s: %v", p, err)
		}
		if !env.Success || len(env.Obj) == 0 {
			t.Fatalf("GET %s envelope invalid: %s", p, rec.Body.String())
		}
	}
}

func TestTrafficController_InvalidParamsHandledCleanly(t *testing.T) {
	engine := newTrafficControllerTestEngine(t)

	badPaths := []string{
		"/panel/api/traffic/overview?period=yearly",
		"/panel/api/traffic/overview?period=day&anchor=not-a-date",
		"/panel/api/traffic/clients?sort=invalid",
		"/panel/api/traffic/node/abc",
		"/panel/api/traffic/node/-1",
		"/panel/api/traffic/node/99999",
	}
	for _, p := range badPaths {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, p, nil)
		engine.ServeHTTP(rec, req)
		var env struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("unmarshal %s: %v", p, err)
		}
		if env.Success {
			t.Fatalf("GET %s expected success=false, got body=%s", p, rec.Body.String())
		}
	}
}

func TestTrafficController_RequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))

	engine := gin.New()
	store := cookie.NewStore([]byte("traffic-auth-secret"))
	engine.Use(sessions.Sessions("3x-ui", store))
	a := &APIController{}
	api := engine.Group("/panel/api")
	api.Use(a.checkAPIAuth)
	api.Use(a.enforceTokenScope)
	NewTrafficController(api.Group("/traffic"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panel/api/traffic/overview", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated XHR status = %d, want 401", rec.Code)
	}
}
