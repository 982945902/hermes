//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model     string        `json:"model,omitempty"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
	Stream    bool          `json:"stream,omitempty"`
}

type result struct {
	latency time.Duration
	status  int
	err     string
}

func main() {
	defaultKey := os.Getenv("HERMES_API_KEY")
	if defaultKey == "" {
		defaultKey = os.Getenv("GATEWAY_API_KEY")
	}
	if defaultKey == "" {
		defaultKey = "change-me-gateway-key"
	}

	var headers headerFlags
	url := flag.String("url", "http://localhost:3000/v1/chat/completions", "chat completions endpoint")
	key := flag.String("key", defaultKey, "API key; empty disables Authorization header")
	model := flag.String("model", "auto", "request model; use auto or empty for gateway auto routing")
	total := flag.Int("n", 100, "total requests")
	concurrency := flag.Int("c", 10, "concurrency")
	timeout := flag.Duration("timeout", 120*time.Second, "per-request timeout")
	prompt := flag.String("prompt", "Say ok in one short sentence.", "user prompt for generated request body")
	maxTokens := flag.Int("max-tokens", 32, "max_tokens for generated request body")
	stream := flag.Bool("stream", false, "enable streaming in generated request body")
	bodyFile := flag.String("body", "", "optional JSON body file; overrides model/prompt/max-tokens/stream")
	insecureTLS := flag.Bool("insecure-tls", false, "skip TLS certificate verification")
	flag.Var(&headers, "H", "extra header, format 'Name: Value'; repeatable")
	flag.Parse()

	if *total <= 0 {
		fatalf("-n must be > 0")
	}
	if *concurrency <= 0 {
		fatalf("-c must be > 0")
	}
	if *concurrency > *total {
		*concurrency = *total
	}

	body, err := requestBody(*bodyFile, *model, *prompt, *maxTokens, *stream)
	if err != nil {
		fatalf("build request body: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        *concurrency * 2,
			MaxIdleConnsPerHost: *concurrency * 2,
			MaxConnsPerHost:     *concurrency * 2,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: *insecureTLS},
		},
	}

	jobs := make(chan int)
	results := make(chan result, *total)
	started := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				results <- doRequest(client, *url, *key, headers, body, *timeout)
			}
		}()
	}

	for i := 0; i < *total; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(results)

	elapsed := time.Since(started)
	printReport(*url, *model, *total, *concurrency, elapsed, results)
}

func requestBody(bodyFile string, model string, prompt string, maxTokens int, stream bool) ([]byte, error) {
	if bodyFile != "" {
		return os.ReadFile(bodyFile)
	}
	payload := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: maxTokens,
		Stream:    stream,
	}
	return json.Marshal(payload)
}

func doRequest(client *http.Client, url string, key string, headers []string, body []byte, timeout time.Duration) result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return result{err: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for _, item := range headers {
		name, value, ok := strings.Cut(item, ":")
		if ok && strings.TrimSpace(name) != "" {
			req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}

	started := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(started)
	if err != nil {
		return result{latency: latency, err: err.Error()}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result{latency: latency, status: resp.StatusCode, err: trimError(data, resp.Status)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return result{latency: latency, status: resp.StatusCode}
}

func printReport(url string, model string, total int, concurrency int, elapsed time.Duration, results <-chan result) {
	var latencies []time.Duration
	statusCodes := map[int]int{}
	errors := map[string]int{}
	success := 0
	failed := 0
	var sum time.Duration

	for item := range results {
		if item.latency > 0 {
			latencies = append(latencies, item.latency)
			sum += item.latency
		}
		if item.status > 0 {
			statusCodes[item.status]++
		}
		if item.err == "" {
			success++
		} else {
			failed++
			errors[item.err]++
		}
	}

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})

	avg := time.Duration(0)
	if len(latencies) > 0 {
		avg = time.Duration(int64(sum) / int64(len(latencies)))
	}

	fmt.Println("Load test result")
	fmt.Printf("url: %s\n", url)
	fmt.Printf("model: %s\n", model)
	fmt.Printf("requests: total=%d success=%d fail=%d concurrency=%d\n", total, success, failed, concurrency)
	fmt.Printf("elapsed: %.3fs\n", elapsed.Seconds())
	fmt.Printf("qps: %.2f\n", float64(total)/elapsed.Seconds())
	fmt.Printf(
		"latency_ms: avg=%.2f tp50=%.2f tp99=%.2f min=%.2f max=%.2f\n",
		ms(avg),
		ms(percentile(latencies, 50)),
		ms(percentile(latencies, 99)),
		ms(percentile(latencies, 0)),
		ms(percentile(latencies, 100)),
	)

	if len(statusCodes) > 0 {
		fmt.Print("status_codes:")
		for _, code := range sortedStatusCodes(statusCodes) {
			fmt.Printf(" %d=%d", code, statusCodes[code])
		}
		fmt.Println()
	}
	if len(errors) > 0 {
		fmt.Println("errors:")
		for _, key := range sortedErrorKeys(errors) {
			fmt.Printf("  %dx %s\n", errors[key], key)
		}
	}
}

func percentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		return values[0]
	}
	if p >= 100 {
		return values[len(values)-1]
	}
	index := int(math.Ceil(float64(len(values))*float64(p)/100.0)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func sortedStatusCodes(values map[int]int) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

func sortedErrorKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if values[keys[i]] != values[keys[j]] {
			return values[keys[i]] > values[keys[j]]
		}
		return keys[i] < keys[j]
	})
	return keys
}

func trimError(data []byte, fallback string) string {
	text := strings.TrimSpace(string(data))
	if text == "" {
		text = fallback
	}
	if len(text) > 240 {
		text = text[:240] + "...(truncated)"
	}
	return text
}

func ms(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000.0
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
