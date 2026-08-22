package sites

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInjectBaseTag(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"with head", "<html><head><title>x</title></head><body>hi</body></html>",
			`<html><head><base href="/spark-proxy/abc/">` + "<title>x</title></head><body>hi</body></html>"},
		{"head with attrs", "<head lang=\"zh\"><meta></head>",
			`<head lang="zh"><base href="/spark-proxy/abc/">` + "<meta></head>"},
		{"no head html", "<div>hello</div>",
			`<base href="/spark-proxy/abc/">` + "<div>hello</div>"},
	}
	for _, c := range cases {
		got := string(injectBaseTag([]byte(c.in), "/spark-proxy/abc/"))
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestRewriteAbsURLs(t *testing.T) {
	in := `<html><head></head><body>
<script src="https://host/app.js"></script>
<link rel="stylesheet" href="https://host/style.css">
<img src="https://host/img.png" srcset="https://host/a.png 1x, https://host/b.png 2x">
<a href="https://other.com/x">x</a>
<div style="background:url('https://host/bg.png')"></div>
<span>text https://not-a-tag should NOT rewrite</span>
</body></html>`
	got := string(rewriteAbsURLs([]byte(in)))
	if strings.Contains(got, "https://host/app.js") {
		t.Fatalf("absolute script URL not rewritten: %s", got)
	}
	if strings.Contains(got, "https://host/style.css") {
		t.Fatalf("absolute css URL not rewritten: %s", got)
	}
	if strings.Contains(got, "https://host/img.png") || strings.Contains(got, "https://host/a.png") {
		t.Fatalf("absolute img/srcset URL not rewritten: %s", got)
	}
	if strings.Contains(got, "https://other.com/x") {
		t.Fatalf("absolute link URL not rewritten: %s", got)
	}
	if strings.Contains(got, "https://host/bg.png") {
		t.Fatalf("absolute style url() not rewritten: %s", got)
	}
	if !strings.Contains(got, "/spark-proxy/") {
		t.Fatalf("no proxy paths found: %s", got)
	}
	// 纯文本中的 URL 不应被改写（改写会破坏页面显示），由 <base> 处理相对链接
	if strings.Contains(got, "/spark-proxy/") && strings.Contains(got, "text https://not-a-tag") {
		if strings.Contains(got, "text /spark-proxy/") {
			t.Fatalf("plain-text URL should not be rewritten: %s", got)
		}
	}
}

// JS 代码（<script> 内容）里的 URL 字符串不得被改写，否则脚本会损坏
func TestRewritePreservesScriptContent(t *testing.T) {
	in := `<html><head></head><body>
<script>
  const API = "https://api.example.com/v1";
  fetch("https://api.example.com/data").then(r => r.json());
  let img = '<img src="https://static.example.com/x.png">';
</script>
<script src="https://cdn.example.com/lib.js"></script>
<p>hi</p>
</body></html>`
	got := string(rewriteHTML([]byte(in), "https://example.com/"))
	if !strings.Contains(got, `const API = "https://api.example.com/v1"`) {
		t.Fatalf("script content corrupted: %s", got)
	}
	if !strings.Contains(got, `fetch("https://api.example.com/data")`) {
		t.Fatalf("script content corrupted: %s", got)
	}
	if !strings.Contains(got, `<img src="https://static.example.com/x.png">`) {
		t.Fatalf("script content corrupted: %s", got)
	}
	// 外部脚本 <script src> 属性必须被改写（否则直连原站仍受证书问题影响）
	if strings.Contains(got, "https://cdn.example.com/lib.js") {
		t.Fatalf("external script src not rewritten: %s", got)
	}
	if !strings.Contains(got, "/spark-proxy/") {
		t.Fatalf("no proxy paths found: %s", got)
	}
}

// 页面自带的 CSP meta 必须被移除，否则会拦截改写后的代理地址资源导致空白
func TestStripCSPMeta(t *testing.T) {
	in := `<html><head>
<meta http-equiv="Content-Security-Policy" content="default-src 'self' https://example.com">
<meta http-equiv="Content-Security-Policy-Report-Only" content="default-src 'self'">
<script src="https://host/app.js"></script>
</head><body>hi</body></html>`
	got := string(rewriteHTML([]byte(in), "https://example.com/"))
	if strings.Contains(strings.ToLower(got), "content-security-policy") {
		t.Fatalf("CSP meta not stripped: %s", got)
	}
	if !strings.Contains(got, "hi") {
		t.Fatalf("page content missing: %s", got)
	}
}

// 带 query 的目标 URL + 相对路径：按浏览器语义解析到目标页面的"目录"下
func TestJoinProxyPath(t *testing.T) {
	cases := []struct {
		target string
		sub    string
		want   string
	}{
		{"https://example.com/", "foo", "https://example.com/foo"},
		{"https://example.com/show_list.php?id=61", "js/main.js", "https://example.com/js/main.js"},
		{"https://example.com/base", "foo/bar", "https://example.com/foo/bar"},
		{"https://example.com/?a=1", "foo", "https://example.com/foo"},
		{"https://example.com/p?x=1&y=2", "sub", "https://example.com/sub"},
		{"https://example.com/a/b/", "c", "https://example.com/a/b/c"},
		{"https://example.com/a/b/page.html", "img/x.png", "https://example.com/a/b/img/x.png"},
	}
	for _, c := range cases {
		got := joinProxyPath(c.target, c.sub)
		if got != c.want {
			t.Errorf("joinProxyPath(%q, %q) = %q, want %q", c.target, c.sub, got, c.want)
		}
	}
}

func TestProxyEndToEnd(t *testing.T) {
	// 目标站点：本地测试服务器，返回带相对链接的 HTML
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<html><head><title>t</title></head><body><a href="sub.html">x</a></body></html>`)
			return
		}
		if r.URL.Path == "/sub.html" {
			_, _ = io.WriteString(w, "sub ok")
			return
		}
		http.NotFound(w, r)
	}))
	defer target.Close()

	svc := &SiteService{}
	proxyURL, err := svc.ProxyUrl(target.URL + "/page")
	if err != nil {
		t.Fatalf("ProxyUrl: %v", err)
	}
	if !strings.HasPrefix(proxyURL, "http://127.0.0.1:") || !strings.Contains(proxyURL, "/spark-proxy/") {
		t.Fatalf("unexpected proxy url: %s", proxyURL)
	}

	resp, err := http.Get(proxyURL)
	if err != nil {
		t.Fatalf("get proxy page: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	s := string(body)
	if !strings.Contains(s, `<base href="/spark-proxy/`) {
		t.Fatalf("base tag not injected: %s", s)
	}
	if !strings.Contains(s, "sub.html") {
		t.Fatalf("original content missing: %s", s)
	}

	// 相对链接应能通过代理继续访问
	subURL := proxyURL + "sub.html"
	resp2, err := http.Get(subURL)
	if err != nil {
		t.Fatalf("get proxy sub page: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(body2) != "sub ok" {
		t.Fatalf("sub page content wrong: %q", string(body2))
	}
}

// 根相对路径（/static/...）必须改写为代理路径：<base> 只能影响不带前导
// 斜杠的相对路径，根相对路径会被浏览器解析到代理服务器根目录而绕开代理
func TestRewriteRootRelative(t *testing.T) {
	in := `<html><head></head><body>
<link rel="stylesheet" href="/static/style.css?v=1">
<script src="/static/app.js"></script>
<img src="/img/a.png" srcset="/img/a.png 1x, /img/b.png 2x">
<a href="/">home</a>
<a href="/page/1">page</a>
<div style="background:url('/img/bg.png')"></div>
<script src="//cdn.example.com/lib.js"></script>
<script src="https://cdn.example.com/lib2.js"></script>
<p>text /plain/path</p>
</body></html>`
	got := string(rewriteHTML([]byte(in), "https://example.com/panel/"))
	if strings.Contains(got, `href="/static/style.css`) {
		t.Fatalf("root-relative css not rewritten: %s", got)
	}
	if strings.Contains(got, `src="/static/app.js"`) {
		t.Fatalf("root-relative js not rewritten: %s", got)
	}
	if strings.Contains(got, `src="/img/a.png"`) || strings.Contains(got, `srcset="/img/a.png`) {
		t.Fatalf("root-relative img/srcset not rewritten: %s", got)
	}
	if strings.Contains(got, `url('/img/bg.png')`) {
		t.Fatalf("root-relative css url not rewritten: %s", got)
	}
	if !strings.Contains(got, `href="/spark-proxy/aHR0cHM6Ly9leGFtcGxlLmNvbS9zdGF0aWMvc3R5bGUuY3NzP3Y9MQ`) {
		t.Fatalf("css proxy path missing: %s", got)
	}
	// 协议相对 // 也要改写（代理页面是 http，//host 会被解析成 http 而失败）；
	// 绝对地址照常改写
	if strings.Contains(got, "//cdn.example.com/lib.js") {
		t.Fatalf("protocol-relative url should be rewritten: %s", got)
	}
	if !strings.Contains(got, "/spark-proxy/aHR0cHM6Ly9jZG4uZXhhbXBsZS5jb20vbGliLmpz/") {
		t.Fatalf("protocol-relative proxy path missing: %s", got)
	}
	if strings.Contains(got, "https://cdn.example.com/lib2.js") {
		t.Fatalf("absolute url should be rewritten: %s", got)
	}
	// 已改写的代理路径不会被二次改写
	if strings.Count(got, "spark-proxy/aHR0cHM6Ly9leGFtcGxlLmNvbS9zdGF0aWMvc3R5bGUuY3NzP3Y9MQ") != 1 {
		t.Fatalf("proxy path duplicated or lost: %s", got)
	}
	// 纯文本里的 /path 不受影响
	if !strings.Contains(got, "text /plain/path") {
		t.Fatalf("plain text changed: %s", got)
	}
}

// CSS 响应中的 url() 根相对路径也要改写
func TestRewriteCSSRootRelative(t *testing.T) {
	in := `body { background: url('/img/bg.png'); }
.icon { background-image: url(https://cdn.example.com/icon.png); }
.logo { background: url("/logo.svg"); }
@font-face { src: url("../font/svgtofont.ttf?t=1731579762035"); }`
	got := string(rewriteCSS([]byte(in), "https://example.com/static/css/login.min.css"))
	if !strings.Contains(got, "spark-proxy/") {
		t.Fatalf("no proxy paths in css: %s", got)
	}
	if strings.Contains(got, "url('/img/bg.png')") {
		t.Fatalf("root-relative url not rewritten: %s", got)
	}
	if strings.Contains(got, "https://cdn.example.com/icon.png") {
		t.Fatalf("absolute url not rewritten: %s", got)
	}
	// ../font/svgtofont.ttf 相对 CSS 目录 /static/css/ 解析 → /static/font/svgtofont.ttf
	if strings.Contains(got, "../font/svgtofont.ttf") {
		t.Fatalf("dot-relative url not rewritten: %s", got)
	}
	if !strings.Contains(got, "spark-proxy/"+base64.RawURLEncoding.EncodeToString([]byte("https://example.com/static/font/svgtofont.ttf?t=1731579762035"))) {
		t.Fatalf("font proxy path missing: %s", got)
	}
}

// 协议相对地址（//host/...）与 srcset 多条目、data-src 懒加载也要改写：
// 代理页面跑在 http://127.0.0.1 上，//host 会被解析成 http://host 而绕开代理
func TestRewriteProtocolRelativeAndSrcset(t *testing.T) {
	in := `<html><head></head><body>
<img srcset="/img/a.png 1x, //cdn.example.com/b.png 2x, https://cdn.example.com/c.png 3x">
<img data-src="/lazy/x.png">
<link rel="stylesheet" href="//cdn.example.com/style.css">
<script data-src="//cdn.example.com/legacy.js"></script>
<div style="background:url('//cdn.example.com/bg.png')"></div>
<a href="//www.bt.cn/help">外部链接</a>
</body></html>`
	got := string(rewriteHTML([]byte(in), "https://example.com/panel/"))
	// srcset 三个条目全部改写（首个根相对、后两个协议相对/绝对）
	for _, want := range []string{
		"/spark-proxy/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWcvYS5wbmc",
		"/spark-proxy/aHR0cHM6Ly9jZG4uZXhhbXBsZS5jb20vYi5wbmc",
		"/spark-proxy/aHR0cHM6Ly9jZG4uZXhhbXBsZS5jb20vYy5wbmc",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("srcset entry not rewritten, missing %s: %s", want, got)
		}
	}
	// data-src 改写
	if !strings.Contains(got, `data-src="/spark-proxy/`) {
		t.Fatalf("data-src not rewritten: %s", got)
	}
	// 属性与 CSS url() 里的协议相对地址改写
	if strings.Contains(got, `href="//cdn.example.com/style.css"`) {
		t.Fatalf("protocol-relative href not rewritten: %s", got)
	}
	if strings.Contains(got, "url('//cdn.example.com/bg.png')") {
		t.Fatalf("protocol-relative css url not rewritten: %s", got)
	}
	// 纯文本/相对路径不受影响
	if !strings.Contains(got, `href="/spark-proxy/aHR0cHM6Ly93d3cuYnQuY24vaGVscA`) {
		t.Fatalf("external protocol-relative link should be proxied too: %s", got)
	}
}

// 含 .. 段的相对路径必须服务端改写：浏览器相对 <base> 解析并归一化时会
// 吃掉代理路径里的编码段（../font/x.ttf → /spark-proxy/font/x.ttf）
func TestRewriteDotRelative(t *testing.T) {
	in := `<html><head></head><body>
<link rel="stylesheet" href="../css/main.css">
<img src="../images/logo.png">
<div style="background:url('../img/bg.png')"></div>
<a href="sub/page.html">普通相对</a>
<a href="#anchor">锚点</a>
</body></html>`
	got := string(rewriteHTML([]byte(in), "https://example.com/panel/show.php?id=1"))
	// 页面目录 /panel/，../css/main.css → https://example.com/css/main.css
	enc := func(u string) string { return "/spark-proxy/" + base64.RawURLEncoding.EncodeToString([]byte(u)) + "/" }
	for _, want := range []string{
		enc("https://example.com/css/main.css"),
		enc("https://example.com/images/logo.png"),
		enc("https://example.com/img/bg.png"),
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dot-relative missing %s: %s", want, got)
		}
	}
	// 普通相对路径与锚点不改写（分别由 <base> 与浏览器自身处理）
	if !strings.Contains(got, `href="sub/page.html"`) || !strings.Contains(got, `href="#anchor"`) {
		t.Fatalf("plain relative / anchor should not be rewritten: %s", got)
	}
}

// 浏览器把 ../ 归一化后请求到代理的"坏编码"路径时，用 Referer 里的页面
// 地址恢复目标（对应真实场景 /spark-proxy/font/svgtofont.ttf）
func TestProxyRecoversDotNormalizedPath(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<html><head></head><body><link rel="stylesheet" href="../font/x.ttf">page</body></html>`)
		case "/font/x.ttf":
			w.Header().Set("Content-Type", "font/ttf")
			_, _ = io.WriteString(w, "font-data")
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	svc := &SiteService{}
	pageProxy, err := svc.ProxyUrl(target.URL + "/page")
	if err != nil {
		t.Fatal(err)
	}

	// 模拟浏览器：../font/x.ttf 相对 base 解析被归一化成 /spark-proxy/font/x.ttf
	broken := strings.Replace(pageProxy, base64.RawURLEncoding.EncodeToString([]byte(target.URL+"/page")), "font", 1) + "x.ttf"
	req, _ := http.NewRequest(http.MethodGet, broken, nil)
	req.Header.Set("Referer", pageProxy)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get broken path: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "font-data" {
		t.Fatalf("recovery failed: status=%d body=%q", resp.StatusCode, body)
	}
}

// JS 动态创建的根相对地址（img.src='/static/icons/logo.svg'）绕过 <base>
// 与 HTML 改写、直接请求到代理服务器根路径时，兜底路由用 Referer 解析目标
func TestProxyCatchAllRootRelative(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<html><head></head><body>page</body></html>`)
		case "/static/icons/logo.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = io.WriteString(w, `<svg/>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	svc := &SiteService{}
	pageProxy, err := svc.ProxyUrl(target.URL + "/page")
	if err != nil {
		t.Fatal(err)
	}
	origin := pageProxy[:strings.Index(pageProxy, "/spark-proxy/")]

	// 模拟 JS 创建的根相对请求：无 /spark-proxy/ 前缀，带页面 Referer
	req, _ := http.NewRequest(http.MethodGet, origin+"/static/icons/logo.svg", nil)
	req.Header.Set("Referer", pageProxy)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get catch-all: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "<svg/>" {
		t.Fatalf("catch-all failed: status=%d body=%q", resp.StatusCode, body)
	}

	// 无 Referer 的请求应 404（不能瞎代理）
	req2, _ := http.NewRequest(http.MethodGet, origin+"/static/unknown.png", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("catch-all without referer should 404, got %d", resp2.StatusCode)
	}
}

// 站点首屏下发会话 Cookie、后续资源必须带 Cookie 才返回时，
// 代理必须在 Go 侧保存并自动携带（否则 SPA 资源 404 → 内嵌页空白）。
// 模拟：/page 下发 Cookie，/asset.js 无 Cookie 时返回 404。
func TestProxyKeepsSessionCookie(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<html><head></head><body><script src="/asset.js"></script>cookie page</body></html>`)
		case "/asset.js":
			if c, err := r.Cookie("sid"); err != nil || c.Value != "abc123" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = io.WriteString(w, "window.ok = 1;")
		default:
			http.NotFound(w, r)
		}
	}))
	defer target.Close()

	svc := &SiteService{}
	proxyURL, err := svc.ProxyUrl(target.URL + "/page")
	if err != nil {
		t.Fatalf("ProxyUrl: %v", err)
	}

	// 1) 打开页面：cookie 应被 jar 保存
	resp, err := http.Get(proxyURL)
	if err != nil {
		t.Fatalf("get proxy page: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "cookie page") {
		t.Fatalf("page content missing: %s", body)
	}

	// 2) 通过代理取子资源：应自动携带 cookie，返回 200 而非 404
	// 页面里 <script src="/asset.js"> 会被改写为独立的代理地址，这里同样
	// 用 ProxyUrl 生成（等价于浏览器改写后的请求）。
	assetURL, err := svc.ProxyUrl(target.URL + "/asset.js")
	if err != nil {
		t.Fatalf("ProxyUrl(asset): %v", err)
	}
	resp2, err := http.Get(assetURL)
	if err != nil {
		t.Fatalf("get proxy asset: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("asset without cookie persistence got %d: %s", resp2.StatusCode, body2)
	}
	if string(body2) != "window.ok = 1;" {
		t.Fatalf("asset content wrong: %q", string(body2))
	}
}

// newSelfSignedTLSServer 起一个使用自签名证书的 HTTPS 服务器（模拟有安全性问题的站点）
func newSelfSignedTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return ts
}

// 有安全性问题（自签名证书）的站点也必须能通过代理打开
func TestProxyInsecureTLS(t *testing.T) {
	target := newSelfSignedTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head></head><body><script src="https://`+r.Host+`/app.js"></script>hello insecure</body></html>`)
	}))
	// 普通客户端应无法直接访问（证书无效）
	if _, err := http.Get(target.URL); err == nil {
		t.Fatal("direct client should fail on self-signed cert")
	}

	svc := &SiteService{}
	proxyURL, err := svc.ProxyUrl(target.URL + "/")
	if err != nil {
		t.Fatalf("ProxyUrl: %v", err)
	}
	resp, err := http.Get(proxyURL)
	if err != nil {
		t.Fatalf("proxy should bypass cert error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	s := string(body)
	if !strings.Contains(s, "hello insecure") {
		t.Fatalf("insecure site content missing: %s", s)
	}
	// 页面里的绝对地址应被改写为代理地址
	if strings.Contains(s, "https://127.0.0.1") {
		t.Fatalf("absolute URL not rewritten: %s", s)
	}
	if !strings.Contains(s, "/spark-proxy/") {
		t.Fatalf("no proxy path in rewritten html: %s", s)
	}
}
