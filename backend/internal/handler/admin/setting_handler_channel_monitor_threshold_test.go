//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingHandler_ChannelMonitorFailureThresholdRoundTripsAndClamps(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	handler := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	body, err := json.Marshal(map[string]any{"channel_monitor_failure_threshold": 101})
	require.NoError(t, err)
	putRecorder := httptest.NewRecorder()
	putContext, _ := gin.CreateTestContext(putRecorder)
	putContext.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(body))
	putContext.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateSettings(putContext)

	require.Equal(t, http.StatusOK, putRecorder.Code)
	require.Equal(t, "100", repo.values[service.SettingKeyChannelMonitorFailureThreshold])

	var putResponse response.Response
	require.NoError(t, json.Unmarshal(putRecorder.Body.Bytes(), &putResponse))
	putData, ok := putResponse.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(100), putData["channel_monitor_failure_threshold"])

	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	handler.GetSettings(getContext)

	require.Equal(t, http.StatusOK, getRecorder.Code)
	var getResponse response.Response
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &getResponse))
	getData, ok := getResponse.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(100), getData["channel_monitor_failure_threshold"])
}
