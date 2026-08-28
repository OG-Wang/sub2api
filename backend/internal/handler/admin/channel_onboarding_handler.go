package admin

import (
	"context"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const channelOnboardingIdempotencyScope = "admin.channel_onboardings.create"

type ChannelOnboardingHandler struct {
	service *service.ChannelOnboardingService
}

func NewChannelOnboardingHandler(svc *service.ChannelOnboardingService) *ChannelOnboardingHandler {
	return &ChannelOnboardingHandler{service: svc}
}

// channelOnboardingCreateRequest keeps only shape/size limits. The accepted
// platform set is not repeated here: service.ChannelOnboardingPlatforms is the
// single source of truth and rejects anything else with a domain error.
type channelOnboardingCreateRequest struct {
	Name                string  `json:"name" binding:"required,max=100"`
	Platform            string  `json:"platform" binding:"required,max=32"`
	RateMultiplier      float64 `json:"rate_multiplier" binding:"required,gt=0"`
	UpstreamBaseURL     string  `json:"upstream_base_url" binding:"required,max=500"`
	UpstreamAPIKey      string  `json:"upstream_api_key" binding:"required,max=2000"`
	PrimaryModel        string  `json:"primary_model" binding:"required,max=200"`
	Concurrency         *int    `json:"concurrency" binding:"omitempty,min=1"`
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
		Concurrency:         req.Concurrency,
		IntervalSeconds:     req.IntervalSeconds,
		ExpectedInputTokens: req.ExpectedInputTokens,
		RequestOrigin:       onboardingRequestOrigin(c),
	}

	// A local variant of executeAdminIdempotentJSON: this endpoint answers 201
	// on a first execution and 200 on a replay, which the shared helper cannot
	// express. The store-unavailable branch mirrors the shared fail-close path
	// so the metric and audit line are not lost.
	var created bool
	result, err := executeAdminIdempotent(c, channelOnboardingIdempotencyScope, req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		created = true
		return h.service.Create(ctx, subject.UserID, serviceReq)
	})
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), channelOnboardingIdempotencyScope, "handler_fail_close")
			logger.LegacyPrintf("handler.idempotency", "[Idempotency] store unavailable: method=%s route=%s scope=%s strategy=fail_close",
				c.Request.Method, c.FullPath(), channelOnboardingIdempotencyScope)
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
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

// onboardingRequestOrigin derives scheme://host from the incoming request.
// It is only a fallback for the monitor endpoint: the service prefers the
// admin-configured api_base_url, because these headers are set by whatever
// proxy sits in front and must not decide where a live API key gets sent.
func onboardingRequestOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := onboardingFirstForwardedValue(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(forwarded)
	}
	host := strings.TrimSpace(c.Request.Host)
	if forwarded := onboardingFirstForwardedValue(c.GetHeader("X-Forwarded-Host")); forwarded != "" {
		host = forwarded
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func onboardingFirstForwardedValue(value string) string {
	if strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}
