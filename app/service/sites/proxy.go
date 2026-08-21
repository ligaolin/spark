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
//   - 同时把 HTML 标签属性 / CSS url() 中的绝对 http(s):// 地址改写为代理
//     地址，否则带绝对路径资源的站点（如 SPA）资源请求仍会直连原站而因
//     证书问题被浏览器拦截，导致页面空白
//   - <script> 内的 JS 代码一律不改写（避免把 JS 字符串里的 URL 破坏掉），
//     <base> 注入会保证 JS 里的相对路径仍走代理
//   - 移除页面自带的 <meta http-equiv="Content-Security-Policy">，避免站点
//     的 CSP 拦截"改写后的代理地址"资源导致整页空白
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
	"net/url"
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
		target = joinProxyPath(target, sub)
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
		// 以最终响应的 URL 作为相对链接基准（自动跟随重定向后仍正确）
		baseURL := target
		if resp.Request != nil && resp.Request.URL != nil {
			baseURL = resp.Request.URL.String()
		}
		body = rewriteHTML(body, baseURL)
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

// joinProxyPath 把相对路径 sub 拼到 target 的路径部分（保留 query/fragment），
// 修复原先直接拼在 URL 末尾导致 "https://host/path?x=1" + "sub" 变成
// "https://host/path?x=1/sub" 的路径错乱。
func joinProxyPath(target, sub string) string {
	u, err := url.Parse(target)
	if err != nil {
		return strings.TrimRight(target, "/") + "/" + sub
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + sub
	return u.String()
}

// ---------- HTML 改写 ----------

// 匹配标签属性里的绝对 URL：src / href / action / poster / data / content / srcset
var attrURLRe = regexp.MustCompile(`(?i)(\s(?:src|href|action|poster|data|content|srcset)\s*=\s*["'])(https?://[^"'\s>]+)`)

// 匹配 CSS url(...) 里的绝对 URL（覆盖 style 属性与 <style> 块）
var cssURLRe = regexp.MustCompile(`(?i)(url\(\s*["']?)(https?://[^)"'\s]+)`)

// 匹配 <script> 元素（含开标签与内容）
var scriptElemRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)

// 匹配 <script> 的开标签（其 src 等属性仍参与 URL 改写）
var scriptOpenRe = regexp.MustCompile(`(?is)<script\b[^>]*>`)

// 匹配页面自带的 CSP meta（阻止其拦截改写后的代理地址资源）
var cspMetaRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?content-security-policy(?:-report-only)?["']?[^>]*>`)

// rewriteHTML 处理 HTML 响应：
//  1. 把 <script> 元素与其余内容分段：元素的开标签（src 等属性）参与 URL
//     改写，脚本内容（JS 代码）原样保留，避免破坏 JS 字符串
//  2. 改写标签属性与 CSS url() 中的绝对 http(s) 地址为代理路径
//  3. 移除 CSP meta、注入 <base>，使相对链接继续走代理
func rewriteHTML(body []byte, baseURL string) []byte {
	var out bytes.Buffer
	last := 0
	for _, m := range scriptElemRe.FindAllIndex(body, -1) {
		out.Write(rewriteAbsURLs(body[last:m[0]]))
		elem := body[m[0]:m[1]]
		if om := scriptOpenRe.FindIndex(elem); om != nil {
			out.Write(rewriteAbsURLs(elem[om[0]:om[1]]))
			out.Write(elem[om[1]:])
		} else {
			out.Write(elem)
		}
		last = m[1]
	}
	out.Write(rewriteAbsURLs(body[last:]))
	res := stripCSPMeta(out.Bytes())
	enc := base64.RawURLEncoding.EncodeToString([]byte(baseURL))
	baseHref := proxyPathPrefix + enc + "/"
	return injectBaseTag(res, baseHref)
}

// rewriteAbsURLs rewrites absolute http(s):// URLs in tag attributes and CSS
// url() expressions to proxy paths. <script> contents are excluded by the
// caller and plain text URLs are left as-is.
func rewriteAbsURLs(body []byte) []byte {
	body = attrURLRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := attrURLRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		u := m[idx[4]:idx[5]]
		out := make([]byte, 0, len(m)+64)
		out = append(out, prefix...)
		out = append(out, proxyPathFor(u)...)
		return out
	})
	body = cssURLRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := cssURLRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		u := m[idx[4]:idx[5]]
		out := make([]byte, 0, len(m)+64)
		out = append(out, prefix...)
		out = append(out, proxyPathFor(u)...)
		return out
	})
	return body
}

// proxyPathFor 把绝对 URL 编码为代理路径 /spark-proxy/<enc>/
func proxyPathFor(u []byte) []byte {
	enc := base64.RawURLEncoding.EncodeToString(u)
	out := make([]byte, 0, len(proxyPathPrefix)+len(enc)+1)
	out = append(out, proxyPathPrefix...)
	out = append(out, enc...)
	out = append(out, '/')
	return out
}

// stripCSPMeta removes <meta http-equiv="Content-Security-Policy"> tags,
// which would otherwise block resources loaded through the proxy.
func stripCSPMeta(body []byte) []byte {
	return cspMetaRe.ReplaceAll(body, nil)
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
