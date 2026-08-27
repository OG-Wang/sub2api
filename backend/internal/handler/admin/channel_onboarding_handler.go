package admin

import (
	"context"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type ChannelOnboardingHandler struct {
	service *service.ChannelOnboardingService
}

func NewChannelOnboardingHandler(svc *service.ChannelOnboardingService) *ChannelOnboardingHandler {
	return &ChannelOnboardingHandler{service: svc}
}

type channelOnboardingCreateRequest struct {
	Name                string  `json:"name" binding:"required,max=100"`
	Platform            string  `json:"platform" binding:"required,oneof=openai anthropic gemini grok kimi zhipu deepseek"`
	RateMultiplier      float64 `json:"rate_multiplier" binding:"required,gt=0"`
	UpstreamBaseURL     string  `json:"upstream_base_url" binding:"required,max=500"`
	UpstreamAPIKey      string  `json:"upstream_api_key" binding:"required,max=2000"`
	PrimaryModel        string  `json:"primary_model" binding:"required,max=200"`
	IntervalSeconds     *int    `json:"interval_seconds" binding:"omitempty,min=15,max=3600"`
	ExpectedInputTokens *int    `json:"expected_input_tokens" binding:"omitempty,gt=0"`
}

// Create POST /api/v1/admin/channel-onboardings
func (h *ChannelOnboardingHandler) Create(c *gin.Context) {
	var req channelOnboardingCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, infraerrors.BadRequest("VALIDATION_ERROR", err.Error()))
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	serviceReq := service.ChannelOnboardingRequest{
		Name:                req.Name,
		Platform:            req.Platform,
		RateMultiplier:      req.RateMultiplier,
		UpstreamBaseURL:     req.UpstreamBaseURL,
		UpstreamAPIKey:      req.UpstreamAPIKey,
		PrimaryModel:        req.PrimaryModel,
		IntervalSeconds:     req.IntervalSeconds,
		ExpectedInputTokens: req.ExpectedInputTokens,
		MonitorEndpoint:     requestServiceOrigin(c),
	}

	var created bool
	result, err := executeAdminIdempotent(c, "admin.channel_onboardings.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		created = true
		return h.service.Create(ctx, subject.UserID, serviceReq)
	})
	if err != nil {
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	if created {
		response.Created(c, result.Data)
		return
	}
	response.Success(c, result.Data)
}

func requestServiceOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := firstForwardedValue(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(forwarded)
	}
	host := strings.TrimSpace(c.Request.Host)
	if forwarded := firstForwardedValue(c.GetHeader("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}

func firstForwardedValue(value string) string {
	if strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}
