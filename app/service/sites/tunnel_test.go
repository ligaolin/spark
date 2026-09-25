package sites

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/sshlib"
)

// TestE2ETunnel 用真实 SSH 主机验证站点管理的「SSH 打开 / SSH 窗口打开」底层隧道：
//
//	SPARK_SITES_E2E_HOST=1.2.3.4 \
//	SPARK_SITES_E2E_KEY=~/.ssh/id_rsa \
//	SPARK_SITES_E2E_TARGET=http://127.0.0.1:80 \
//	go test ./app/service/sites/ -run TestE2ETunnel -v
//
// 不设置环境变量时自动跳过。测试会把 %AppData% 重定向到临时目录，
// 数据库与 known_hosts 都不会碰到用户真实数据，结束后自动清理隧道。
func TestE2ETunnel(t *testing.T) {
	host := os.Getenv("SPARK_SITES_E2E_HOST")
	if host == "" {
		t.Skip("设置 SPARK_SITES_E2E_HOST / SPARK_SITES_E2E_KEY 后开启")
	}
	keyPath := os.Getenv("SPARK_SITES_E2E_KEY")
	if keyPath == "" {
		t.Skip("未设置 SPARK_SITES_E2E_KEY（私钥路径）")
	}
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = home + keyPath[1:]
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Skipf("读取私钥失败：%v", err)
	}
	sshPort := 22
	if p, err := strconv.Atoi(os.Getenv("SPARK_SITES_E2E_SSH_PORT")); err == nil && p > 0 {
		sshPort = p
	}
	user := os.Getenv("SPARK_SITES_E2E_USER")
	if user == "" {
		user = "root"
	}
	// 目标用「服务器自己」的服务：隧道在服务器侧拨号，所以 127.0.0.1 是服务器的回环
	targetURL := os.Getenv("SPARK_SITES_E2E_TARGET")
	if targetURL == "" {
		targetURL = "http://127.0.0.1:80/"
	}

	// 隔离数据目录：DB 与 known_hosts 都写到临时目录
	t.Setenv("AppData", t.TempDir())
	if err := db.InitDB(); err != nil {
		t.Fatalf("初始化临时数据库失败：%v", err)
	}
	// 必须在 t.TempDir 的删除动作之前关掉 SQLite 连接，否则 Windows 下文件被占用、
	// 临时目录清理会失败（t.Cleanup 后进先出，这里注册得比 TempDir 晚就会先执行）
	t.Cleanup(func() {
		if sqlDB, err := db.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.GetDB().AutoMigrate(&model.SavedConnection{}); err != nil {
		t.Fatalf("建表失败：%v", err)
	}

	key, err := sshlib.ProbeHostKey(host, sshPort)
	if err != nil {
		t.Skipf("获取主机密钥失败：%v", err)
	}
	if err := sshlib.SaveHostKey(host, sshPort, key); err != nil {
		t.Fatalf("写入 known_hosts 失败：%v", err)
	}

	conn := model.SavedConnection{
		Name: "e2e-ssh", Type: "ssh", Host: host, Port: sshPort,
		Username: user, UseKey: true, PrivateKey: string(pemBytes),
	}
	if err := db.GetDB().Create(&conn).Error; err != nil {
		t.Fatalf("写入测试连接失败：%v", err)
	}

	// 心跳调快，便于验证「SSH 断开后隧道自动摘除」
	oldKA := tunnelKeepAlive
	tunnelKeepAlive = 300 * time.Millisecond
	t.Cleanup(func() { tunnelKeepAlive = oldKA })

	svc := &SiteService{}
	t.Cleanup(func() {
		for _, info := range svc.ListTunnels() {
			_ = svc.CloseTunnel(info.ID)
		}
	})

	// 1) 建立隧道并真的把流量转发过去
	first, err := svc.OpenTunnel(conn.ID, targetURL)
	if err != nil {
		t.Fatalf("OpenTunnel 失败：%v", err)
	}
	t.Logf("隧道已建立：%s → %s（连接 %s）", first.LocalURL, first.Target, first.ConnectionName)
	resp := httpGet(t, first.LocalURL)
	t.Logf("经隧道取回：%s", resp)
	if !strings.HasPrefix(resp, "HTTP/") {
		t.Fatalf("经隧道没有拿到 HTTP 响应：%q", resp)
	}

	// 2) 同一「SSH 连接 + 目标」必须复用隧道（原来每点一次都会新建连接+端口）
	second, err := svc.OpenTunnel(conn.ID, targetURL)
	if err != nil {
		t.Fatalf("第二次 OpenTunnel 失败：%v", err)
	}
	if second.LocalURL != first.LocalURL {
		t.Errorf("同一连接+目标应复用隧道，实际拿到不同地址：%s vs %s", first.LocalURL, second.LocalURL)
	}
	if second.ID != first.ID {
		t.Errorf("应复用同一个隧道 ID：%s vs %s", second.ID, first.ID)
	}
	if n := len(svc.ListTunnels()); n != 1 {
		t.Errorf("复用后活动隧道数应为 1，实际 %d", n)
	}

	// 3) 同一目标的其它路径复用同一端口（标签页按 URL 去重才不会被无谓拆开）
	other, err := svc.OpenTunnel(conn.ID, withPath(targetURL, "/other"))
	if err != nil {
		t.Fatalf("不同路径 OpenTunnel 失败：%v", err)
	}
	if localPort(t, other.LocalURL) != localPort(t, first.LocalURL) {
		t.Errorf("同一目标的不同路径应共用端口：%s vs %s", other.LocalURL, first.LocalURL)
	}
	if !strings.HasSuffix(other.LocalURL, "/other") {
		t.Errorf("本地 URL 应带上请求的路径：%s", other.LocalURL)
	}

	// 4) 换一个 SSH 连接应该是另一条隧道
	conn2 := model.SavedConnection{
		Name: "e2e-ssh-2", Type: "ssh", Host: host, Port: sshPort,
		Username: user, UseKey: true, PrivateKey: string(pemBytes),
	}
	if err := db.GetDB().Create(&conn2).Error; err != nil {
		t.Fatalf("写入第二个连接失败：%v", err)
	}
	third, err := svc.OpenTunnel(conn2.ID, targetURL)
	if err != nil {
		t.Fatalf("换连接 OpenTunnel 失败：%v", err)
	}
	if third.ID == first.ID {
		t.Error("不同 SSH 连接不应复用同一隧道")
	}
	if n := len(svc.ListTunnels()); n != 2 {
		t.Errorf("活动隧道数应为 2，实际 %d", n)
	}

	// 5) 关闭后端口可复用：重开同一条隧道应回到同一个本地地址
	firstPort := localPort(t, first.LocalURL)
	if err := svc.CloseTunnel(first.ID); err != nil {
		t.Fatalf("CloseTunnel 失败：%v", err)
	}
	if n := len(svc.ListTunnels()); n != 1 {
		t.Errorf("关闭后活动隧道数应为 1，实际 %d", n)
	}
	reopened, err := svc.OpenTunnel(conn.ID, targetURL)
	if err != nil {
		t.Fatalf("重开隧道失败：%v", err)
	}
	if got := localPort(t, reopened.LocalURL); got != firstPort {
		t.Logf("提示：重开后端口从 %d 变为 %d（原端口被占用时会回退到随机端口）", firstPort, got)
	}

	// 6) SSH 连接断开后，隧道应自动从活动列表消失（原来会一直挂着）
	tunnelMu.Lock()
	tun := tunnels[reopened.ID]
	tunnelMu.Unlock()
	if tun == nil {
		t.Fatal("找不到刚建立的隧道")
	}
	_ = tun.client.Close()
	waitUntil(t, 6*time.Second, func() bool {
		for _, info := range svc.ListTunnels() {
			if info.ID == reopened.ID {
				return false
			}
		}
		return true
	}, "SSH 断开后隧道应被自动摘除")
}

// httpGet 通过隧道地址发一个最简单的 HTTP 请求，返回响应首行。
func httpGet(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("解析本地 URL 失败：%v", err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 8*time.Second)
	if err != nil {
		t.Fatalf("连接隧道端口失败：%v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	req := fmt.Sprintf("GET %s HTTP/1.0\r\nHost: %s\r\nConnection: close\r\n\r\n", pathOf(u), u.Host)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("写入请求失败：%v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && line == "" {
		t.Fatalf("读取响应失败：%v", err)
	}
	return strings.TrimSpace(line)
}

func pathOf(u *url.URL) string {
	p := u.RequestURI()
	if p == "" {
		return "/"
	}
	return p
}

func withPath(rawURL, path string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = path
	return u.String()
}

func localPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("解析本地 URL 失败：%v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("本地 URL 没有端口：%s", rawURL)
	}
	return p
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s（等待 %s 超时）", msg, timeout)
}

var _ = http.StatusOK
