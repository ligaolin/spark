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
// 请求方法（GET / POST / …）与请求体透传，cookie 登录态由 jar 自动携带；
// 响应只转发少量安全头，X-Frame-Options / Content-Security-Policy 等反嵌入头
// 不会被带回浏览器，因此自签名、反 iframe 的内网站点也能嵌入打开。
// 已知限制：WebSocket 升级尚未隧道转发。
package sites

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
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
//
// 带 cookie jar：很多站点（如宝塔面板）首屏会下发一个 HttpOnly 会话
// Cookie，后续的 JS / CSS / 接口请求必须携带它才返回 200（不带则 404，
// SPA 无法启动 → 内嵌页空白）。jar 按目标域名隔离存储，代理在 Go 侧
// 自动携带，不需要（也不能）把这些 Cookie 塞给浏览器 iframe。
var insecureClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 用户显式开启
	},
	Jar: mustCookieJar(),
}

// mustCookieJar 创建 cookie jar（失败时返回 nil，仅退化为无 Cookie 行为）。
func mustCookieJar() http.CookieJar {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil
	}
	return jar
}

// fallbackUserAgent 兜底 UA：部分站点（如宝塔面板）会按 UA 判断是否放行
// （非浏览器 UA 直接 404）。iframe 请求一般会带 WebView2 的 UA，这里只在
// 请求没有 UA 时兜底，避免 Go 默认的 "Go-http-client/1.1" 被拒。
const fallbackUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

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
		// 兜底路由：页面 JS 动态创建的根相对 / 协议相对地址（如
		// /static/icons/x.svg）会绕过 <base> 与 HTML 改写、直接请求到
		// 代理服务器根路径，用 Referer 里的页面地址解析目标
		mux.HandleFunc("/", handleCatchAll)
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
	recovered := false
	target := ""
	if err == nil && strings.Contains(string(targetBytes), "://") {
		target = string(targetBytes)
	} else {
		// 编码段不是合法 URL：通常是页面里的 ../ 相对路径被浏览器归一化时
		// 吃掉了编码段（如 /spark-proxy/<enc>/../font/x.ttf → /spark-proxy/font/x.ttf）。
		// 用 Referer 里的页面代理地址作为基准恢复目标。
		if page := pageURLFromReferer(r.Header.Get("Referer")); page != "" {
			// 请求路径 enc/sub 相对页面目录解析（等价于页面里的 ../ 相对路径）
			target = joinProxyPath(page, enc+"/"+sub)
			recovered = true
		}
		if !recovered {
			http.Error(w, "bad target encoding", http.StatusBadRequest)
			return
		}
	}
	if !recovered && sub != "" {
		target = joinProxyPath(target, sub)
	}
	serveTarget(w, r, target)
}

// handleCatchAll 兜底处理所有非 /spark-proxy/ 路径的请求。页面 JS 动态创建
// 的根相对地址（如 img.src = '/static/icons/x.svg'）会绕过 <base> 与 HTML
// 改写、直接请求到代理服务器根路径（127.0.0.1:PORT/static/...），这里用
// Referer 里的页面地址把请求路径解析成目标 URL 后走同一代理逻辑。
func handleCatchAll(w http.ResponseWriter, r *http.Request) {
	page := pageURLFromReferer(r.Header.Get("Referer"))
	if page == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	sub := strings.TrimPrefix(r.URL.Path, "/")
	if sub == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	target := joinProxyPath(page, sub)
	serveTarget(w, r, target)
}

// pageURLFromReferer 从代理页面的 Referer 里提取目标页面 URL。
func pageURLFromReferer(ref string) string {
	if ref == "" {
		return ""
	}
	i := strings.Index(ref, proxyPathPrefix)
	if i < 0 {
		return ""
	}
	rest := strings.TrimPrefix(ref[i+len(proxyPathPrefix):], "/")
	enc := rest
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		enc = rest[:j]
	}
	if b, err := base64.RawURLEncoding.DecodeString(enc); err == nil && strings.Contains(string(b), "://") {
		return string(b)
	}
	return ""
}

// serveTarget 抓取 target 并返回给浏览器（HTML/CSS 做 URL 改写）。
func serveTarget(w http.ResponseWriter, r *http.Request, target string) {
	// 子资源自身的 query（如 /js/main.js?v=2、file.php?id=5）透传给目标
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}

	// 透传请求体：登录表单 / XHR 等 POST 必须把 method + body 原样转发，
	// 否则目标站会按错误方法处理（宝塔面板登录 POST 被当 GET → 404）。
	var reqBody io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		b, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			http.Error(w, "read body failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, reqBody)
	if err != nil {
		http.Error(w, "bad target url", http.StatusBadRequest)
		return
	}
	// 把用户浏览器的常见请求头透传过去，尽量保持站点行为一致
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	} else {
		req.Header.Set("User-Agent", fallbackUserAgent)
	}
	for _, h := range []string{"Accept", "Content-Type", "X-Requested-With"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
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
	// 以最终响应的 URL 作为相对链接基准（自动跟随重定向后仍正确）
	baseURL := target
	if resp.Request != nil && resp.Request.URL != nil {
		baseURL = resp.Request.URL.String()
	}
	if isHTMLContent(ct) {
		body = rewriteHTML(body, baseURL)
	} else if isCSSText(ct) {
		body = rewriteCSS(body, baseURL)
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

func isCSSText(ct string) bool {
	return strings.Contains(strings.ToLower(ct), "text/css")
}

// joinProxyPath 把相对子路径 sub 按浏览器语义解析到 target 的"目录"下
// （而非直接拼在完整路径末尾）：
//   - 页面 https://host/show_list.php?id=61 里的 js/main.js → https://host/js/main.js
//   - 页面 https://host/a/b/page.html 里的 c.js → https://host/a/b/c.js
// 同时丢弃 target 自身的 query/fragment（相对路径解析时不会继承页面的 query）。
func joinProxyPath(target, sub string) string {
	u, err := url.Parse(target)
	if err != nil {
		// 解析失败时退化为目录拼接
		if i := strings.LastIndex(target, "/"); i >= 0 {
			return target[:i+1] + sub
		}
		return target + "/" + sub
	}
	dir := u.Path
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i+1]
	} else {
		dir = "/"
	}
	u.Path = dir + sub
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// ---------- HTML 改写 ----------

// 匹配标签属性里的绝对 URL：src / href / action / poster / data / content / srcset
// （含 data-src / data-original 等常见懒加载资源属性）
var attrURLRe = regexp.MustCompile(`(?i)(\s(?:src|href|action|poster|data|content|srcset|data-src|data-original)\s*=\s*["'])(https?://[^"'\s>]+)`)

// 匹配 CSS url(...) 里的绝对 URL（覆盖 style 属性与 <style> 块）
var cssURLRe = regexp.MustCompile(`(?i)(url\(\s*["']?)(https?://[^)"'\s]+)`)

// 匹配标签属性值（不做内容限制，是否改写由回调判断）
var attrValueRe = regexp.MustCompile(`(?i)(\s(?:src|href|action|poster|data|content|srcset|data-src|data-original)\s*=\s*["'])([^"'\s>]+)`)

// 匹配 CSS url(...) 的值（不做内容限制，是否改写由回调判断）
var cssValueRe = regexp.MustCompile(`(?i)(url\(\s*["']?)([^)"'\s]+)`)

// 匹配 srcset 完整值（多条目，如 "/a.png 1x, /b.png 2x"）
var srcsetValueRe = regexp.MustCompile(`(?i)(\ssrcset\s*=\s*["'])([^"']*)`)

// 匹配 <script> 元素（含开标签与内容）
var scriptElemRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)

// 匹配 <script> 的开标签（其 src 等属性仍参与 URL 改写）
var scriptOpenRe = regexp.MustCompile(`(?is)<script\b[^>]*>`)

// 匹配页面自带的 CSP meta（阻止其拦截改写后的代理地址资源）
var cspMetaRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?content-security-policy(?:-report-only)?["']?[^>]*>`)

// proxyPathPrefixBytes 代理路径前缀（[]byte 形式，供快速判断）
var proxyPathPrefixBytes = []byte(proxyPathPrefix)

// isRootRelative 判断值是否为根相对路径：以单个 / 开头（排除 // 协议相对、
// 以及已改写过的 /spark-proxy/ 代理路径）。
func isRootRelative(v []byte) bool {
	if len(v) == 0 || v[0] != '/' {
		return false
	}
	if len(v) > 1 && v[1] == '/' {
		return false
	}
	return !bytes.HasPrefix(v, proxyPathPrefixBytes)
}

// isProtocolRelative 判断值是否为协议相对地址（//host/...）。
func isProtocolRelative(v []byte) bool {
	return len(v) > 2 && v[0] == '/' && v[1] == '/'
}

// rewriteHTML 处理 HTML 响应：
//  1. 把 <script> 元素与其余内容分段：元素的开标签（src 等属性）参与 URL
//     改写，脚本内容（JS 代码）原样保留，避免破坏 JS 字符串
//  2. 改写标签属性与 CSS url() 中的绝对 http(s) 地址、根相对路径
//     （/static/...）以及含 .. 段的相对路径：<base> 只能影响不带前导斜杠
//     的相对路径；根相对路径会被解析到代理服务器根目录而绕开代理；含 ..
//     的相对路径会被浏览器归一化时吃掉代理路径里的编码段，两者都必须显式
//     改写为代理地址
//  3. 移除 CSP meta、注入 <base>，使普通相对链接继续走代理
func rewriteHTML(body []byte, baseURL string) []byte {
	var out bytes.Buffer
	last := 0
	for _, m := range scriptElemRe.FindAllIndex(body, -1) {
		out.Write(rewriteURLs(body[last:m[0]], baseURL))
		elem := body[m[0]:m[1]]
		if om := scriptOpenRe.FindIndex(elem); om != nil {
			out.Write(rewriteURLs(elem[om[0]:om[1]], baseURL))
			out.Write(elem[om[1]:])
		} else {
			out.Write(elem)
		}
		last = m[1]
	}
	out.Write(rewriteURLs(body[last:], baseURL))
	res := stripCSPMeta(out.Bytes())
	enc := base64.RawURLEncoding.EncodeToString([]byte(baseURL))
	baseHref := proxyPathPrefix + enc + "/"
	return injectBaseTag(res, baseHref)
}

// rewriteCSS 处理 CSS 响应：改写 url() 里的绝对地址、根相对路径与含 ..
// 的相对路径。
func rewriteCSS(body []byte, baseURL string) []byte {
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
	body = cssValueRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := cssValueRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		v := m[idx[4]:idx[5]]
		target := rewriteURLValue(v, baseURL)
		if target == nil {
			return m
		}
		out := make([]byte, 0, len(m)+64)
		out = append(out, prefix...)
		out = append(out, target...)
		return out
	})
	return body
}

// originOf 返回 URL 的 scheme://host（含端口），用于把根相对路径拼成完整地址。
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// rewriteURLs 依次改写绝对地址与根相对/协议相对/含 .. 的路径。
func rewriteURLs(body []byte, baseURL string) []byte {
	body = rewriteAbsURLs(body)
	body = rewriteRootURLs(body, baseURL)
	return body
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

// rewriteRootURLs 把标签属性与 CSS url() 里的根相对路径（/xxx）、协议相对
// 地址（//host/...）和含 .. 段的相对路径改写为代理路径。普通相对路径（由
// <base> 处理）与 /spark-proxy/ 本身不改写。
// 协议相对与 .. 路径必须改写：代理页面运行在 http://127.0.0.1 上，协议相对
// 会被解析成 http://host/...（绕过代理且协议错误）；.. 路径会被浏览器归一化
// 时吃掉代理路径里的编码段，静态资源必然加载失败。
func rewriteRootURLs(body []byte, baseURL string) []byte {
	if baseURL == "" {
		return body
	}
	body = attrValueRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := attrValueRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		v := m[idx[4]:idx[5]]
		target := rewriteURLValue(v, baseURL)
		if target == nil {
			return m
		}
		out := make([]byte, 0, len(m)+64)
		out = append(out, prefix...)
		out = append(out, target...)
		return out
	})
	body = cssValueRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := cssValueRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		v := m[idx[4]:idx[5]]
		target := rewriteURLValue(v, baseURL)
		if target == nil {
			return m
		}
		out := make([]byte, 0, len(m)+64)
		out = append(out, prefix...)
		out = append(out, target...)
		return out
	})
	body = rewriteSrcset(body, baseURL)
	return body
}

// isDotRelative 判断值是否为含 .. 段的纯相对路径（这类路径被浏览器相对
// <base> 解析时会归一化掉代理路径里的编码段，必须服务端改写）。
func isDotRelative(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	first := v[0]
	if first == '/' || first == '#' || first == '?' {
		return false
	}
	// 带 scheme 的（mailto:、data:、javascript: 等）不改写
	if i := bytes.IndexByte(v, ':'); i > 0 {
		schemeOK := true
		for j := 0; j < i; j++ {
			c := v[j]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.') {
				schemeOK = false
				break
			}
		}
		if schemeOK {
			return false
		}
	}
	for _, seg := range strings.Split(string(v), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// rewriteURLValue 返回 v 改写后的代理路径；无需改写（普通相对路径、data:、
// 已改写的代理路径等）时返回 nil。
func rewriteURLValue(v []byte, baseURL string) []byte {
	switch {
	case bytes.HasPrefix(v, []byte("http://")) || bytes.HasPrefix(v, []byte("https://")):
		// 绝对地址（主要供 srcset 多条目使用；普通属性已被 attrURLRe 先改写）
		return proxyPathFor(v)
	case isRootRelative(v):
		return proxyPathFor(append([]byte(originOf(baseURL)), v...))
	case isProtocolRelative(v):
		// //host/path → https://host/path（原页面为 https）
		return proxyPathFor(append([]byte("https:"), v...))
	case isDotRelative(v):
		// ../xxx → 按页面目录解析成绝对地址再改写
		base, err := url.Parse(baseURL)
		if err != nil {
			return nil
		}
		rel, err := url.Parse(string(v))
		if err != nil {
			return nil
		}
		return proxyPathFor([]byte(base.ResolveReference(rel).String()))
	}
	return nil
}

// rewriteSrcset 逐个改写 srcset 多条目里的 URL（跳过已改写的代理路径）。
func rewriteSrcset(body []byte, baseURL string) []byte {
	return srcsetValueRe.ReplaceAllFunc(body, func(m []byte) []byte {
		idx := srcsetValueRe.FindSubmatchIndex(m)
		if len(idx) < 4 {
			return m
		}
		prefix := m[idx[2]:idx[3]]
		val := string(m[idx[4]:idx[5]])
		parts := strings.Split(val, ",")
		changed := false
		for i, p := range parts {
			trimmed := strings.TrimLeft(p, " \t")
			sp := strings.IndexAny(trimmed, " \t")
			if sp < 0 {
				sp = len(trimmed)
			}
			urlPart := trimmed[:sp]
			target := rewriteURLValue([]byte(urlPart), baseURL)
			if target == nil {
				continue
			}
			parts[i] = strings.Replace(p, urlPart, string(target), 1)
			changed = true
		}
		if !changed {
			return m
		}
		out := make([]byte, 0, len(m)+128)
		out = append(out, prefix...)
		out = append(out, strings.Join(parts, ",")...)
		return out
	})
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
