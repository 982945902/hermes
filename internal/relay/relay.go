package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/cache"
	"github.com/982945902/hermes/internal/health"
	"github.com/982945902/hermes/internal/identity"
	"github.com/982945902/hermes/internal/model"
	"github.com/982945902/hermes/internal/provider"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	cache    *cache.ChannelCache
	provider *provider.OpenAICompatible
	meter    *health.Meter
	guard    identity.Guard
}

type chatEnvelope struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func NewHandler(channelCache *cache.ChannelCache, provider *provider.OpenAICompatible, meter *health.Meter, guard identity.Guard) *Handler {
	return &Handler{cache: channelCache, provider: provider, meter: meter, guard: guard}
}

func (h *Handler) ListModels(c *gin.Context) {
	models := h.cache.ListModels()
	allowedModels := make([]string, 0, len(models))
	for _, name := range models {
		if tokenAllowsModel(c, name) {
			allowedModels = append(allowedModels, name)
		}
	}
	data := make([]gin.H, 0, len(allowedModels)+1)
	now := time.Now().Unix()
	if len(allowedModels) > 0 {
		data = append(data, modelItem("auto", now))
	}
	for _, name := range allowedModels {
		data = append(data, modelItem(name, now))
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

func modelItem(name string, created int64) gin.H {
	return gin.H{
		"id":       name,
		"object":   "model",
		"created":  created,
		"owned_by": "hermes",
	}
}

func (h *Handler) ChatCompletions(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		openAIError(c, http.StatusBadRequest, "failed to read request body")
		return
	}
	var envelope chatEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		openAIError(c, http.StatusBadRequest, "invalid JSON body")
		return
	}

	requestedModel := strings.TrimSpace(envelope.Model)
	autoModel := requestedModel == "" || strings.EqualFold(requestedModel, "auto")
	var routes []model.ChannelRoute
	if autoModel {
		allRoutes := h.cache.ListRoutes()
		if len(allRoutes) == 0 {
			openAIError(c, http.StatusNotFound, "no models are available")
			return
		}
		routes = filterRoutesByToken(c, allRoutes)
		if len(routes) == 0 {
			openAIError(c, http.StatusForbidden, "no models are allowed by this API key")
			return
		}
	} else {
		if !tokenAllowsModel(c, requestedModel) {
			openAIError(c, http.StatusForbidden, "model is not allowed by this API key")
			return
		}
		routes = h.cache.FindByModel(requestedModel)
		if len(routes) == 0 {
			openAIError(c, http.StatusNotFound, "model is not available")
			return
		}
	}
	if h.guard.IsProbeRequest(body) {
		if envelope.Stream {
			c.Header("Content-Type", "text/event-stream")
			c.Status(http.StatusOK)
			_, _ = c.Writer.Write(h.guard.FixedStreamReply())
			if flusher, ok := c.Writer.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}
		c.JSON(http.StatusOK, h.guard.FixedReply())
		return
	}
	body, err = h.guard.InjectSystemPrompt(body)
	if err != nil {
		openAIError(c, http.StatusBadRequest, "failed to inject identity guard")
		return
	}

	ordered := h.meter.Rank(routes)
	if len(ordered) == 0 {
		openAIError(c, http.StatusServiceUnavailable, "all matching channels are currently unavailable")
		return
	}

	var lastErr error
	for _, route := range ordered {
		channel := route.Channel
		upstreamModel := route.UpstreamModel
		req, err := h.provider.BuildChatRequest(c.Request.Context(), channel, body, upstreamModel)
		if err != nil {
			lastErr = err
			continue
		}
		started := time.Now()
		resp, err := h.provider.Do(req)
		latency := time.Since(started)
		if err != nil {
			h.meter.Record(channel, route.ExternalModel, upstreamModel, latency, 0, false, 0, err.Error())
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errText := copyUpstreamError(resp)
			h.meter.Record(channel, route.ExternalModel, upstreamModel, latency, resp.StatusCode, false, 0, errText.Error())
			lastErr = errText
			if c.Writer.Written() {
				return
			}
			continue
		}
		h.meter.Record(channel, route.ExternalModel, upstreamModel, latency, resp.StatusCode, true, estimateQuality(resp), "")
		h.probeUnavailable(c, routes, route.Key(), body)
		proxyResponse(c, resp, h.guard)
		return
	}
	if lastErr == nil {
		lastErr = errors.New("no channel returned a response")
	}
	openAIError(c, http.StatusBadGateway, lastErr.Error())
}

func (h *Handler) probeUnavailable(c *gin.Context, routes []model.ChannelRoute, selectedKey string, body []byte) {
	probes := h.meter.ProbeCandidates(routes, selectedKey)
	for _, route := range probes {
		route := route
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			req, err := h.provider.BuildChatRequest(ctx, route.Channel, body, route.UpstreamModel)
			if err != nil {
				h.meter.Record(route.Channel, route.ExternalModel, route.UpstreamModel, 0, 0, false, 0, err.Error())
				return
			}
			started := time.Now()
			resp, err := h.provider.Do(req)
			latency := time.Since(started)
			if err != nil {
				h.meter.Record(route.Channel, route.ExternalModel, route.UpstreamModel, latency, 0, false, 0, err.Error())
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
			success := resp.StatusCode >= 200 && resp.StatusCode < 300
			errText := ""
			if !success {
				errText = resp.Status
			}
			h.meter.Record(route.Channel, route.ExternalModel, route.UpstreamModel, latency, resp.StatusCode, success, boolQuality(success), errText)
		}()
	}
}

func estimateQuality(resp *http.Response) float64 {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0.78
	}
	return 0
}

func boolQuality(success bool) float64 {
	if success {
		return 0.78
	}
	return 0
}

func tokenAllowsModel(c *gin.Context, modelName string) bool {
	if !c.GetBool("api_token_model_limits_enabled") {
		return true
	}
	raw, ok := c.Get("api_token_model_limits")
	if !ok {
		return false
	}
	limits, ok := raw.(map[string]bool)
	if !ok {
		return false
	}
	return limits[strings.TrimSpace(modelName)]
}

func filterRoutesByToken(c *gin.Context, routes []model.ChannelRoute) []model.ChannelRoute {
	out := make([]model.ChannelRoute, 0, len(routes))
	for _, route := range routes {
		if tokenAllowsModel(c, route.ExternalModel) {
			out = append(out, route)
		}
	}
	return out
}

func proxyResponse(c *gin.Context, resp *http.Response, guard identity.Guard) {
	defer resp.Body.Close()
	for key, values := range resp.Header {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Status(resp.StatusCode)
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		data, _ := io.ReadAll(resp.Body)
		_, _ = c.Writer.Write(guard.SanitizeJSON(data))
		return
	}
	flusher, canFlush := c.Writer.(http.Flusher)
	sanitizer := guard.NewStreamSanitizer()
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			out := sanitizer.Write(buffer[:n])
			if len(out) > 0 {
				_, _ = c.Writer.Write(out)
			}
			if canFlush && len(out) > 0 {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	if out := sanitizer.Flush(); len(out) > 0 {
		_, _ = c.Writer.Write(out)
		if canFlush {
			flusher.Flush()
		}
	}
}

func copyUpstreamError(resp *http.Response) error {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(data) == 0 {
		return errors.New(resp.Status)
	}
	var compact bytes.Buffer
	if json.Compact(&compact, data) == nil {
		data = compact.Bytes()
	}
	return errors.New(string(data))
}

func openAIError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "hermes_error",
			"code":    "gateway_error",
		},
	})
}
