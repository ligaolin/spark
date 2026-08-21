// 站点内嵌浏览的本地代理：绕过浏览器对 SSL 证书的校验。
//
// WebView2 对证书无效 / 自签名 / 过期等站点会在 iframe 里直接显示错误页，
// 前端无法绕过。这里在 127.0.0.1 上起一个本地 HTTP 代理，用忽略证书校验的
// Go HTTP 客户端去抓取目标页面，再把内容回给 iframe，从而让"有安全性问题"
// 的站点也能打开。
//
// 路由约定：/spark-proxy/<base64url>/<相对路径...>
//   - <base64url> 是目标 URL（含协议与查询串）的 base64url 编码
//   - 相对路径会拼到目标 URL 末尾（页面内相对链接通过注入 <base> 解析回来）
//   - 对 HTML 响应注入 <base href="/spark-proxy/<base64url>/">，
//     使页面里的相对链接（img/script/a）继续走代理
//   - 同时把 HTML 中所有绝对 http(s):// 地址改写为代理地址，否则带绝对路径
//     资源的站点（如 SPA）资源请求仍会直连原站而因证书问题被浏览器拦截，
//     导致页面空白
//
// 已知限制：POST 表单 / cookie 登录态不会透传（每个请求独立抓取）。对自签名
// 内网站点足够用。
package sites

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const proxyPathPrefix = "/spark-proxy/"

var (
	proxyOnce sync.Once
	proxyBase string
	proxyErr  error
)

// 匹配 HTML 文本中的绝对 URL（协议 + 域名/地址，不含标签边界字符）
var absURLRe = regexp.MustCompile(`(?i)https?://[^\s"'<>()]+`)

// insecureClient 跳过 TLS 证书校验（自签名 / 过期 / 无效证书等场景）。
// 仅用于用户明确选择"忽略证书"的内嵌浏览。
var insecureClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 用户显式开启
	},
}

// ProxyBase returns the local proxy base URL, starting the proxy on first use.
func (s *SiteService) ProxyBase() (string, error) {
	proxyOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			proxyErr = err
			return
		}
		proxyBase = "http://" + ln.Addr().String()
		mux := http.NewServeMux()
		mux.HandleFunc(proxyPathPrefix, handleProxy)
		go func() { _ = http.Serve(ln, mux) }()
	})
	return proxyBase, proxyErr
}

// ProxyUrl converts a raw URL into a proxy URL for the embedded iframe.
func (s *SiteService) ProxyUrl(raw string) (string, error) {
	base, err := s.ProxyBase()
	if err != nil {
		return "", err
	}
	u := normalizeURL(raw)
	if u == "" {
		return "", errors.New("链接地址不能为空")
	}
	enc := base64.RawURLEncoding.EncodeToString([]byte(u))
	return base + proxyPathPrefix + enc + "/", nil
}

// handleProxy fetches the target URL through the insecure client and returns
// its content, injecting a <base> tag and rewriting absolute URLs for HTML.
func handleProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, proxyPathPrefix)
	enc, sub := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		enc, sub = rest[:i], rest[i+1:]
	}
	if enc == "" {
		http.Error(w, "missing target", http.StatusBadRequest)
		return
	}
	targetBytes, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		http.Error(w, "bad target encoding", http.StatusBadRequest)
		return
	}
	target := string(targetBytes)
	if sub != "" {
		target = strings.TrimRight(target, "/") + "/" + sub
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad target url", http.StatusBadRequest)
		return
	}
	// 把用户浏览器的常见请求头透传过去，尽量保持站点行为一致
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := insecureClient.Do(req)
	if err != nil {
		http.Error(w, "proxy fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 上限 64MB，防止拖垮本地代理
	if err != nil {
		http.Error(w, "proxy read failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if isHTMLContent(ct) {
		baseHref := proxyPathPrefix + enc + "/"
		// 1) 绝对 http(s) 地址 → 代理地址（资源也走忽略证书的抓取）
		body = rewriteAbsURLs(body)
		// 2) 注入 <base>，相对链接继续走代理
		body = injectBaseTag(body, baseHref)
	}
	for _, h := range []string{"Content-Type", "Cache-Control", "Content-Language", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func isHTMLContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml")
}

// rewriteAbsURLs rewrites every absolute http(s):// URL in the HTML to a
// proxy path, so the browser fetches those resources through this proxy
// (which ignores TLS errors) instead of hitting the origin directly.
func rewriteAbsURLs(body []byte) []byte {
	return absURLRe.ReplaceAllFunc(body, func(m []byte) []byte {
		enc := base64.RawURLEncoding.EncodeToString(m)
		return []byte(proxyPathPrefix + enc + "/")
	})
}

// injectBaseTag inserts a <base> element right after the opening <head> (or
// before <html> when <head> is missing). Per HTML spec the first <base> wins,
// so injecting before any existing one makes relative links go through proxy.
func injectBaseTag(body []byte, baseHref string) []byte {
	base := []byte(`<base href="` + baseHref + `">`)
	if i := bytes.Index(body, []byte("<head")); i >= 0 {
		// 找到 <head ...> 的结束 >，在其后插入
		if j := bytes.IndexByte(body[i:], '>'); j >= 0 {
			pos := i + j + 1
			out := make([]byte, 0, len(body)+len(base)+1)
			out = append(out, body[:pos]...)
			out = append(out, base...)
			out = append(out, body[pos:]...)
			return out
		}
	}
	if i := bytes.Index(body, []byte("<html")); i >= 0 {
		if j := bytes.IndexByte(body[i:], '>'); j >= 0 {
			pos := i + j + 1
			out := make([]byte, 0, len(body)+len(base)+1)
			out = append(out, body[:pos]...)
			out = append(out, base...)
			out = append(out, body[pos:]...)
			return out
		}
	}
	// 没有 head/html 标签（纯文本片段等），直接前置，多数页面仍可工作
	out := make([]byte, 0, len(body)+len(base))
	out = append(out, base...)
	out = append(out, body...)
	return out
}
