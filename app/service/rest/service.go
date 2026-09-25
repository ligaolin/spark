// Package rest provides a built-in HTTP client (like curl or Postman) that
// executes requests through the Go backend, bypassing browser CORS restrictions.
package rest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"changeme/app/service/types"
)

// RestService exposes an HTTP request proxy to the frontend.
type RestService struct{}

// ServiceName implements application.ServiceName.
func (s *RestService) ServiceName() string { return "RestService" }

// Send makes an HTTP request and returns the response synchronously.
// All requests go through the Go net/http client, so there are no CORS
// limitations from the WebView.
func (s *RestService) Send(req types.RestRequest) types.RestResponse {
	start := time.Now()

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    "URL 不能为空",
		}
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}

	var bodyReader io.Reader
	if req.Body != "" {
		bodyReader = bytes.NewReader([]byte(req.Body))
	}

	httpReq, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    fmt.Sprintf("构造请求失败: %v", err),
		}
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "Spark/1.0")
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    fmt.Sprintf("请求失败: %v", err),
		}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return types.RestResponse{
			Status:     resp.StatusCode,
			StatusText: resp.Status,
			Duration:   time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("读取响应体失败: %v", err),
		}
	}

	headers := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	return types.RestResponse{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    headers,
		Body:       string(bodyBytes),
		Duration:   time.Since(start).Milliseconds(),
	}
}
