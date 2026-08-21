package sites

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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

// 带 query 的目标 URL + 相对路径：子路径应拼在路径部分，而不是 query 之后
func TestJoinProxyPath(t *testing.T) {
	cases := []struct {
		target string
		sub    string
		want   string
	}{
		{"https://example.com/", "foo", "https://example.com/foo"},
		{"https://example.com/base", "foo/bar", "https://example.com/base/foo/bar"},
		{"https://example.com/?a=1", "foo", "https://example.com/foo?a=1"},
		{"https://example.com/p?x=1&y=2", "sub", "https://example.com/p/sub?x=1&y=2"},
		{"https://example.com/a/b/", "c", "https://example.com/a/b/c"},
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
		if r.URL.Path == "/page/sub.html" {
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
