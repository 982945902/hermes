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

	"github.com/982945902/hermes/internal/health"
	"github.com/982945902/hermes/internal/model"
	"github.com/982945902/hermes/internal/provider"
	"github.com/982945902/hermes/internal/store"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	store    *store.Store
	provider *provider.OpenAICompatible
	meter    *health.Meter
}

type chatEnvelope struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func NewHandler(store *store.Store, provider *provider.OpenAICompatible, meter *health.Meter) *Handler {
	return &Handler{store: store, provider: provider, meter: meter}
}

func (h *Handler) ListModels(c *gin.Context) {
	models, err := h.store.ListPublicModels(c.Request.Context())
	if err != nil {
		openAIError(c, http.StatusInternalServerError, "failed to list models")
		return
	}
	data := make([]gin.H, 0, len(models))
	now := time.Now().Unix()
	for _, name := range models {
		data = append(data, gin.H{
			"id":       name,
			"object":   "model",
			"created":  now,
			"owned_by": "hermes",
		})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
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
	if strings.TrimSpace(envelope.Model) == "" {
		openAIError(c, http.StatusBadRequest, "model is required")
		return
	}

	channels, err := h.store.FindChannelsForModel(c.Request.Context(), envelope.Model)
	if err != nil {
		openAIError(c, http.StatusInternalServerError, "failed to select channel")
		return
	}
	if len(channels) == 0 {
		openAIError(c, http.StatusNotFound, "model is not available")
		return
	}
	ordered := h.meter.Rank(channels)
	if len(ordered) == 0 {
		openAIError(c, http.StatusServiceUnavailable, "all matching channels are currently unavailable")
		return
	}

	var lastErr error
	for _, channel := range ordered {
		upstreamModel := channel.UpstreamModel(envelope.Model)
		req, err := h.provider.BuildChatRequest(c.Request.Context(), channel, body, upstreamModel)
		if err != nil {
			lastErr = err
			continue
		}
		started := time.Now()
		resp, err := h.provider.Do(req)
		latency := time.Since(started)
		if err != nil {
			h.meter.Record(channel, latency, 0, false, 0, err.Error())
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errText := copyUpstreamError(resp)
			h.meter.Record(channel, latency, resp.StatusCode, false, 0, errText.Error())
			lastErr = errText
			if c.Writer.Written() {
				return
			}
			continue
		}
		h.meter.Record(channel, latency, resp.StatusCode, true, estimateQuality(resp), "")
		h.probeUnavailable(c, channels, channel.ID.Hex(), body, envelope.Model)
		proxyResponse(c, resp)
		return
	}
	if lastErr == nil {
		lastErr = errors.New("no channel returned a response")
	}
	openAIError(c, http.StatusBadGateway, lastErr.Error())
}

func (h *Handler) probeUnavailable(c *gin.Context, channels []model.Channel, selectedID string, body []byte, modelName string) {
	probes := h.meter.ProbeCandidates(channels, selectedID)
	for _, channel := range probes {
		channel := channel
		upstreamModel := channel.UpstreamModel(modelName)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			req, err := h.provider.BuildChatRequest(ctx, channel, body, upstreamModel)
			if err != nil {
				h.meter.Record(channel, 0, 0, false, 0, err.Error())
				return
			}
			started := time.Now()
			resp, err := h.provider.Do(req)
			latency := time.Since(started)
			if err != nil {
				h.meter.Record(channel, latency, 0, false, 0, err.Error())
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256*1024))
			success := resp.StatusCode >= 200 && resp.StatusCode < 300
			errText := ""
			if !success {
				errText = resp.Status
			}
			h.meter.Record(channel, latency, resp.StatusCode, success, boolQuality(success), errText)
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

func proxyResponse(c *gin.Context, resp *http.Response) {
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
	flusher, canFlush := c.Writer.(http.Flusher)
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if n > 0 {
			_, _ = c.Writer.Write(buffer[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
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
