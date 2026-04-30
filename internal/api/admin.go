package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/auth"
	"github.com/982945902/hermes/internal/cache"
	"github.com/982945902/hermes/internal/health"
	"github.com/982945902/hermes/internal/model"
	"github.com/982945902/hermes/internal/provider"
	"github.com/982945902/hermes/internal/store"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type AdminHandler struct {
	store    *store.Store
	cache    *cache.ChannelCache
	auth     *auth.Service
	provider *provider.OpenAICompatible
	meter    *health.Meter
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type channelTestRequest struct {
	Prompt        string `json:"prompt"`
	Model         string `json:"model"`
	UpstreamModel string `json:"upstream_model"`
}

type fetchModelsRequest struct {
	ID           string            `json:"id"`
	Provider     string            `json:"provider"`
	BaseURL      string            `json:"base_url"`
	APIKey       string            `json:"api_key"`
	ExtraHeaders map[string]string `json:"extra_headers"`
}

func NewAdminHandler(store *store.Store, channelCache *cache.ChannelCache, auth *auth.Service, provider *provider.OpenAICompatible, meter *health.Meter) *AdminHandler {
	return &AdminHandler{store: store, cache: channelCache, auth: auth, provider: provider, meter: meter}
}

func (h *AdminHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	token, err := h.auth.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (h *AdminHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"username": c.GetString("admin_username")})
}

func (h *AdminHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":                   "ok",
		"time":                     time.Now(),
		"channel_cache_updated_at": h.cache.UpdatedAt(),
	})
}

func (h *AdminHandler) Barometer(c *gin.Context) {
	c.JSON(http.StatusOK, h.meter.Snapshot(h.cache.ListChannels()))
}

func (h *AdminHandler) ListChannels(c *gin.Context) {
	channels, err := h.store.ListChannels(c.Request.Context(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list channels"})
		return
	}
	result := make([]model.Channel, len(channels))
	for i, channel := range channels {
		result[i] = channel.Public(true)
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AdminHandler) GetChannel(c *gin.Context) {
	channel, err := h.store.GetChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if err == mongo.ErrNoDocuments {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": "channel not found"})
		return
	}
	c.JSON(http.StatusOK, channel.Public(true))
}

func (h *AdminHandler) CreateChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel"})
		return
	}
	if err := validateChannel(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.CreateChannel(c.Request.Context(), &channel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
		return
	}
	h.cache.ReloadAsync()
	c.JSON(http.StatusCreated, channel.Public(true))
}

func (h *AdminHandler) UpdateChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel"})
		return
	}
	if err := validateChannel(&channel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.UpdateChannel(c.Request.Context(), c.Param("id"), &channel); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update channel"})
		return
	}
	h.cache.ReloadAsync()
	updated, _ := h.store.GetChannel(c.Request.Context(), c.Param("id"))
	if updated == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, updated.Public(true))
}

func (h *AdminHandler) DeleteChannel(c *gin.Context) {
	if err := h.store.DeleteChannel(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete channel"})
		return
	}
	h.cache.ReloadAsync()
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) TestChannel(c *gin.Context) {
	channel, err := h.store.GetChannel(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	var req channelTestRequest
	_ = c.ShouldBindJSON(&req)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	modelName := req.Model
	if modelName == "" && len(channel.Models) > 0 {
		modelName = channel.Models[0]
	}
	upstreamModel := strings.TrimSpace(req.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = channel.UpstreamModel(modelName)
	}
	started := time.Now()
	result, testErr := h.provider.Test(ctx, *channel, modelName, upstreamModel, req.Prompt)
	latency := time.Since(started)
	lastError := ""
	if testErr != nil {
		lastError = testErr.Error()
	}
	statusCode := 200
	if testErr != nil {
		statusCode = statusFromTestError(lastError)
	}
	h.meter.Record(*channel, modelName, upstreamModel, latency, statusCode, testErr == nil, boolQuality(testErr == nil), lastError)
	_ = h.store.UpdateChannelTestResult(c.Request.Context(), c.Param("id"), lastError)
	h.cache.ReloadAsync()
	if testErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": lastError})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "response": result})
}

func statusFromTestError(message string) int {
	const prefix = "upstream returned "
	if !strings.HasPrefix(message, prefix) {
		return 0
	}
	codeText, _, _ := strings.Cut(strings.TrimPrefix(message, prefix), ":")
	code, err := strconv.Atoi(strings.TrimSpace(codeText))
	if err != nil {
		return 0
	}
	return code
}

func boolQuality(success bool) float64 {
	if success {
		return 0.78
	}
	return 0
}

func (h *AdminHandler) FetchModels(c *gin.Context) {
	var req fetchModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	apiKey := req.APIKey
	if req.ID != "" && (strings.TrimSpace(apiKey) == "" || strings.Contains(apiKey, "********")) {
		existing, err := h.store.GetChannel(c.Request.Context(), req.ID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		apiKey = existing.APIKey
	}
	channel := model.Channel{
		Provider:     req.Provider,
		BaseURL:      req.BaseURL,
		APIKey:       apiKey,
		ExtraHeaders: req.ExtraHeaders,
	}
	channel.Normalize()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	models, err := h.provider.FetchModels(ctx, channel)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": models})
}

func validateChannel(channel *model.Channel) error {
	channel.Normalize()
	if channel.Name == "" {
		return errString("name is required")
	}
	if channel.BaseURL == "" {
		return errString("base_url is required")
	}
	if channel.APIKey == "" {
		return errString("api_key is required")
	}
	if len(channel.Models) == 0 {
		return errString("at least one model is required")
	}
	return nil
}

type errString string

func (e errString) Error() string {
	return string(e)
}
