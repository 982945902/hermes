package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/model"
	"github.com/982945902/hermes/internal/provider"
	"github.com/982945902/hermes/internal/store"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	store    *store.Store
	provider *provider.OpenAICompatible
}

type chatEnvelope struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func NewHandler(store *store.Store, provider *provider.OpenAICompatible) *Handler {
	return &Handler{store: store, provider: provider}
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
	orderChannels(channels)

	var lastErr error
	for _, channel := range channels {
		upstreamModel := channel.UpstreamModel(envelope.Model)
		req, err := h.provider.BuildChatRequest(c.Request.Context(), channel, body, upstreamModel)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := h.provider.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = copyUpstreamError(c, resp)
			if c.Writer.Written() {
				return
			}
			continue
		}
		proxyResponse(c, resp)
		return
	}
	if lastErr == nil {
		lastErr = errors.New("no channel returned a response")
	}
	openAIError(c, http.StatusBadGateway, lastErr.Error())
}

func orderChannels(channels []model.Channel) {
	sort.SliceStable(channels, func(i, j int) bool {
		return channels[i].Priority > channels[j].Priority
	})
	for start := 0; start < len(channels); {
		end := start + 1
		for end < len(channels) && channels[end].Priority == channels[start].Priority {
			end++
		}
		weightedShuffle(channels[start:end])
		start = end
	}
}

func weightedShuffle(channels []model.Channel) {
	for i := range channels {
		j := chooseWeighted(channels[i:])
		channels[i], channels[i+j] = channels[i+j], channels[i]
	}
}

func chooseWeighted(channels []model.Channel) int {
	total := 0
	for _, channel := range channels {
		weight := channel.Weight
		if weight <= 0 {
			weight = 1
		}
		total += weight
	}
	if total <= 0 {
		return rand.Intn(len(channels))
	}
	pick := rand.Intn(total)
	for i, channel := range channels {
		weight := channel.Weight
		if weight <= 0 {
			weight = 1
		}
		if pick < weight {
			return i
		}
		pick -= weight
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

func copyUpstreamError(c *gin.Context, resp *http.Response) error {
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
