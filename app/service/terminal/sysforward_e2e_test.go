package terminal

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"changeme/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// TestE2ESSHSystemForward 走完整链路验证系统转发：真实 SSH 连接 → 会话命令通道
// （runCommandWithTimeout：独立 exec channel + sudo 包装）→ 探测 / 创建 / 列出 /
// 删除。两种目标：
//
//	A) 指定一台真实主机（真机验证用）：
//	   SPARK_E2E_HOST=1.2.3.4 SPARK_E2E_KEY=~/.ssh/id_rsa SPARK_E2E_USER=root \
//	   go test ./app/service/terminal/ -run TestE2ESSHSystemForward -v
//	B) 本机 WSL 临时起一个 sshd：SPARK_E2E_SSH=1
//
// 注意：本测试会真的在目标机上增删防火墙规则（用一个空闲高位端口 + 文档地址
// 192.0.2.x 作目标，且把写文件路径重定向到 /tmp），结束时清理。只在自己有权
// 操作的机器上跑。
func TestE2ESSHSystemForward(t *testing.T) {
	host := os.Getenv("SPARK_E2E_HOST")
	if host == "" && os.Getenv("SPARK_E2E_SSH") == "" {
		t.Skip("设置 SPARK_E2E_HOST=<主机>（或 SPARK_E2E_SSH=1 用 WSL sshd）后开启")
	}

	var (
		client  *xssh.Client
		cleanup func()
	)
	if host != "" {
		client = dialE2EHost(t, host)
	} else {
		client, cleanup = dialWSLSSHD(t)
	}
	defer client.Close()
	if cleanup != nil {
		defer cleanup()
	}

	runE2E(t, sshRunner(client), client, host)
}

// runE2E 是两种目标共用的测试主体。dialHost 是「从本机访问目标机」用的地址
// （真实主机=可达的公网地址，WSL=127.0.0.1），只有做转发连通性验证时才用得到。
func runE2E(t *testing.T, run fwRun, client *xssh.Client, dialHost string) {
	const keyID = "_e2e"
	svc := &TerminalService{}
	svc.ensure()

	// 注入一个会话：系统转发只用到底层 client 的命令通道，不涉及前端事件层
	sess := &sshSession{id: keyID, client: client, stopKA: make(chan struct{})}
	svc.mu.Lock()
	svc.sessions[keyID] = sess
	svc.mu.Unlock()

	isolateFWPaths(t)
	// 必须用 defer 而不是 t.Cleanup：t.Cleanup 在测试函数（含 defer client.Close()）
	// 之后才执行，那时 SSH 连接已经关了，清理脚本根本发不出去。
	defer func() {
		if out, code, err := run(fwCleanupScript); err != nil || code != 0 {
			t.Errorf("清理脚本执行异常（code=%d err=%v）：%s", code, err, out)
		} else if !strings.Contains(out, "cleaned") {
			t.Errorf("清理脚本未见完成标记：%s", out)
		}
	}()

	// 1) 探测：真实 SSH 通道 + 真实防火墙后端
	st, err := svc.SystemForwardStatus(keyID)
	if err != nil {
		t.Fatalf("SystemForwardStatus 失败：%v", err)
	}
	t.Logf("后端=%s 可用=%v 权限=%s ip_forward=%v 区域=%q 持久化=%s 规则数=%d 提示=%q",
		st.Backend, st.Available, st.Privilege, st.IPForward, st.Zone, st.Persist, st.Total, st.Message)
	for _, r := range st.Rules {
		t.Logf("  已有规则: %s %d → %s:%d  managed=%v  raw=%s",
			strings.ToUpper(r.Proto), r.SrcPort, r.DestIP, r.DestPort, r.Managed, r.Raw)
	}
	if st.Backend == fwBackendNone || !st.Available {
		t.Skipf("远端没有可用的防火墙后端或权限：%s", st.Message)
	}
	preManaged := st.Managed

	// 2) 选一个内核确认空闲的高位端口，避免碰到线上服务
	srcPort := pickFreePort(t, run)

	// 3) 创建 → 列表 → 删除（目标用 TEST-NET-1 文档地址，不会真的打到任何服务）
	req := types.SystemForwardRequest{
		Proto: "tcp", SrcPort: srcPort, DestIP: "192.0.2.123", DestPort: 8443, Note: "e2e",
	}
	st, err = svc.AddSystemForward(keyID, req)
	if err != nil {
		t.Fatalf("AddSystemForward 失败：%v", err)
	}
	var found *types.SystemForward
	for i := range st.Rules {
		if st.Rules[i].SrcPort == srcPort {
			found = &st.Rules[i]
		}
	}
	if found == nil {
		t.Fatalf("创建后未在列表里找到规则：%+v", st.Rules)
	}
	if !found.Managed || found.Note != "e2e" || found.DestIP != "192.0.2.123" {
		t.Fatalf("规则信息不完整（备注/来源未从远端清单读回）：%+v", *found)
	}
	if !st.IPForward && st.Backend != fwBackendFirewalld {
		t.Errorf("创建规则后应已开启 ip_forward：%+v", st)
	}

	// 4) 内核里必须真的有配套规则
	assertBackendHasRule(t, run, st.Backend, srcPort, true)

	// 5) 删除并确认清理干净
	st, err = svc.RemoveSystemForward(keyID, found.ID)
	if err != nil {
		t.Fatalf("RemoveSystemForward 失败：%v", err)
	}
	for _, r := range st.Rules {
		if r.SrcPort == srcPort {
			t.Fatalf("删除后规则仍在列表里：%+v", r)
		}
	}
	assertBackendHasRule(t, run, st.Backend, srcPort, false)
	// 本应用创建的规则被删光后，自建的链 / 表应当一并撤掉，不留下垃圾
	if preManaged == 0 {
		assertOwnStructuresGone(t, run, st.Backend)
	}

	// 6) 参数校验与错误会话也要通过真实服务返回
	if _, err := svc.AddSystemForward(keyID, types.SystemForwardRequest{
		Proto: "tcp", SrcPort: srcPort, DestIP: "127.0.0.1", DestPort: 80,
	}); err == nil || !strings.Contains(err.Error(), "回环") {
		t.Fatalf("回环目标应被拒绝，实际：%v", err)
	}
	if _, err := svc.SystemForwardStatus("no-such-session"); err == nil {
		t.Fatal("不存在的会话应返回错误")
	}

	// 7) 可选：真的连一次转发端口，确认流量确实被转发（而不只是规则存在）
	if probe := os.Getenv("SPARK_E2E_PROBE_TARGET"); probe != "" && dialHost != "" {
		verifyForwarding(t, svc, keyID, run, dialHost, srcPort, probe)
	}
	// 8) 可选：云安全组通常只放行少数端口，外面连不上不代表 DNAT 没生效。
	//    用一对 veth + netns 在目标机内部造一个「外部客户端」，直接验证转发。
	if localPort := os.Getenv("SPARK_E2E_PROBE_LOCAL_PORT"); localPort != "" {
		verifyForwardingViaNetns(t, svc, keyID, run, srcPort, localPort)
	}
	// 9) 可选：跨主机转发（目标是另一个 netns 里的服务）。这条路径才会真正走到
	//    MASQUERADE 与 FORWARD 放行，是「转发到别的机器」场景的关键验证。
	if os.Getenv("SPARK_E2E_PROBE_XNET") != "" {
		verifyCrossForward(t, svc, keyID, run, srcPort)
	}
}

const (
	e2eNetns   = "sparkfwtest"
	e2eVethH   = "spark-vh"
	e2eVethN   = "spark-vn"
	e2eNetnsIP = "10.77.0.2"
	e2eHostIP  = "10.77.0.1"

	e2eSrvNs     = "sparksrvtest"
	e2eSrvVethH  = "spark-sh"
	e2eSrvVethN  = "spark-sn"
	e2eSrvIP     = "10.78.0.2"
	e2eSrvHostIP = "10.78.0.1"
	e2eSrvPort   = 12345
)

// e2eNetnsTeardown 清掉测试造的 netns / veth / 临时服务。
// netns 是 bind mount，里面还有进程时 ip netns del 会失败，所以先强杀进程、
// 留出退出时间，最后用 lazy umount 兜底，否则残留会挡住下一次测试。
func e2eNetnsTeardown(run fwRun) {
	run(`pkill -9 -f "http.server ` + strconv.Itoa(e2eSrvPort) + `" 2>/dev/null
sleep 0.5
for ns in ` + e2eNetns + ` ` + e2eSrvNs + `; do
  ip netns pids "$ns" 2>/dev/null | xargs -r kill -9 2>/dev/null
  sleep 0.3
  ip netns del "$ns" 2>/dev/null
  if [ -e "/run/netns/$ns" ]; then
    umount -l "/run/netns/$ns" 2>/dev/null
    rm -f "/run/netns/$ns" 2>/dev/null
  fi
done
ip link del ` + e2eVethH + ` 2>/dev/null
ip link del ` + e2eSrvVethH + ` 2>/dev/null
rm -f /tmp/spark-srv.log
true`)
}

// verifyCrossForward 造两个 netns（客户端 / 服务端），把转发端口指向服务端 netns，
// 再从客户端 netns 访问。走的是 DNAT → FORWARD → MASQUERADE 的完整转发路径。
func verifyCrossForward(t *testing.T, svc *TerminalService, keyID string, run fwRun, srcPort int) {
	t.Helper()
	e2eNetnsTeardown(run)
	defer e2eNetnsTeardown(run)

	setup := fmt.Sprintf(`
set -e
ip netns add %[1]s
ip link add %[2]s type veth peer name %[3]s
ip link set %[3]s netns %[1]s
ip addr add %[4]s/24 dev %[2]s
ip link set %[2]s up
ip netns exec %[1]s ip addr add %[5]s/24 dev %[3]s
ip netns exec %[1]s ip link set %[3]s up
ip netns exec %[1]s ip link set lo up
ip netns add %[6]s
ip link add %[7]s type veth peer name %[8]s
ip link set %[8]s netns %[6]s
ip addr add %[9]s/24 dev %[7]s
ip link set %[7]s up
ip netns exec %[6]s ip addr add %[10]s/24 dev %[8]s
ip netns exec %[6]s ip link set %[8]s up
ip netns exec %[6]s ip link set lo up
ip netns exec %[6]s nohup python3 -m http.server %[11]d --bind %[10]s >/tmp/spark-srv.log 2>&1 &
sleep 1
echo xnet-ready
`, e2eNetns, e2eVethH, e2eVethN, e2eHostIP, e2eNetnsIP,
		e2eSrvNs, e2eSrvVethH, e2eSrvVethN, e2eSrvHostIP, e2eSrvIP, e2eSrvPort)
	if out, code, err := run(setup); err != nil || !strings.Contains(out, "xnet-ready") {
		t.Skipf("无法搭建跨主机测试环境（code=%d err=%v）：%s", code, err, out)
	}

	st, err := svc.AddSystemForward(keyID, types.SystemForwardRequest{
		Proto: "tcp", SrcPort: srcPort, DestIP: e2eSrvIP, DestPort: e2eSrvPort, Note: "e2e-xnet",
	})
	if err != nil {
		t.Fatalf("创建跨主机转发失败：%v", err)
	}
	var ruleID string
	for _, r := range st.Rules {
		if r.SrcPort == srcPort {
			ruleID = r.ID
		}
	}
	if ruleID == "" {
		t.Fatalf("未找到刚创建的转发规则：%+v", st.Rules)
	}
	defer func() {
		if _, err := svc.RemoveSystemForward(keyID, ruleID); err != nil {
			t.Errorf("清理跨主机转发规则失败：%v", err)
		}
	}()

	// 客户端 netns → 宿主 veth:转发端口 → 服务端 netns:12345
	cmd := fmt.Sprintf(
		`ip netns exec %s timeout 8 bash -c 'exec 3<>/dev/tcp/%s/%d && printf "GET / HTTP/1.0\r\n\r\n" >&3 && head -c 24 <&3' 2>&1; echo "[exit=$?]"`,
		e2eNetns, e2eHostIP, srcPort)
	out, _, _ := run(cmd)
	t.Logf("跨主机转发结果（%s:%d → %s:%d）：%s", e2eHostIP, srcPort, e2eSrvIP, e2eSrvPort, strings.TrimSpace(out))
	if !strings.Contains(out, "HTTP/1.0 200") {
		diag, _, _ := run("iptables -w -t nat -S 2>/dev/null | grep -E 'DNAT|MASQUERADE' | head -5; " +
			"nft list ruleset 2>/dev/null | grep -iE 'dnat|masquerade' | head -5; " +
			"cat /proc/sys/net/ipv4/ip_forward")
		t.Fatalf("跨主机转发未生效（期望 HTTP/1.0 200）。诊断：\n%s", diag)
	}
	t.Logf("跨主机转发验证通过：流量经 DNAT → FORWARD → MASQUERADE 到达 %s:%d", e2eSrvIP, e2eSrvPort)
}

// verifyForwardingViaNetns 在目标机上建 veth + netns，从 netns 里连转发端口，
// 目标选目标机自己已放行的端口（例如 22）。这样不依赖云安全组，直接验证 DNAT。
func verifyForwardingViaNetns(t *testing.T, svc *TerminalService, keyID string, run fwRun, srcPort int, localPort string) {
	t.Helper()
	port, err := strconv.Atoi(localPort)
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("SPARK_E2E_PROBE_LOCAL_PORT 无效：%q", localPort)
	}
	out, _, _ := run("ip -o -4 addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1")
	destIP := strings.TrimSpace(out)
	if net.ParseIP(destIP) == nil {
		t.Skipf("无法确定目标机主 IP：%q", out)
	}

	setup := fmt.Sprintf(`
ip netns del %[1]s 2>/dev/null
ip link del %[2]s 2>/dev/null
ip netns add %[1]s || exit 1
ip link add %[2]s type veth peer name %[3]s || exit 1
ip link set %[3]s netns %[1]s
ip addr add %[4]s/24 dev %[2]s
ip link set %[2]s up
ip netns exec %[1]s ip addr add %[5]s/24 dev %[3]s
ip netns exec %[1]s ip link set %[3]s up
ip netns exec %[1]s ip link set lo up
echo netns-ready
`, e2eNetns, e2eVethH, e2eVethN, e2eHostIP, e2eNetnsIP)
	if out, code, err := run(setup); err != nil || !strings.Contains(out, "netns-ready") {
		t.Skipf("目标机不支持 netns（跳过内部连通性验证，code=%d err=%v）：%s", code, err, out)
	}
	defer run(`ip netns del ` + e2eNetns + ` 2>/dev/null; ip link del ` + e2eVethH + ` 2>/dev/null; true`)

	st, err := svc.AddSystemForward(keyID, types.SystemForwardRequest{
		Proto: "tcp", SrcPort: srcPort, DestIP: destIP, DestPort: port, Note: "e2e-netns",
	})
	if err != nil {
		t.Fatalf("创建转发（netns 验证）失败：%v", err)
	}
	var ruleID string
	for _, r := range st.Rules {
		if r.SrcPort == srcPort {
			ruleID = r.ID
		}
	}
	if ruleID == "" {
		t.Fatalf("未找到刚创建的转发规则：%+v", st.Rules)
	}
	defer func() {
		if _, err := svc.RemoveSystemForward(keyID, ruleID); err != nil {
			t.Errorf("清理 netns 验证规则失败：%v", err)
		}
	}()

	cmd := fmt.Sprintf(
		`ip netns exec %s timeout 6 bash -c 'exec 3<>/dev/tcp/%s/%d && head -c 40 <&3' 2>&1; echo "[exit=$?]"`,
		e2eNetns, e2eHostIP, srcPort)
	out, _, _ = run(cmd)
	t.Logf("netns 侧连 %s:%d → %s:%d 结果：%s", e2eHostIP, srcPort, destIP, port, strings.TrimSpace(out))
	if !strings.Contains(out, "SSH-2.0") {
		diag, _, _ := run("nft list ruleset 2>/dev/null | grep -iE 'dnat|to :|redirect' | head -8")
		t.Fatalf("内部验证失败：经转发的连接没有返回预期内容（期望 SSH banner）。诊断：\n%s", diag)
	}
	t.Logf("内部连通性验证通过：%s:%d → %s:%d 流量确实被 DNAT 转发", e2eHostIP, srcPort, destIP, port)
}

// verifyForwarding 把 srcPort 转发到 probe（host:port）指定的服务，然后从本机
// 连一次转发端口。能连上（并读到同样内容）才说明 DNAT 真的生效。
// 需要目标服务对本机可达（例如转发到服务器的 ssh 端口 22）。
func verifyForwarding(t *testing.T, svc *TerminalService, keyID string, run fwRun, dialHost string, srcPort int, probe string) {
	t.Helper()
	destHost, destPortStr, err := net.SplitHostPort(probe)
	if err != nil {
		t.Fatalf("SPARK_E2E_PROBE_TARGET 格式应为 host:port：%v", err)
	}
	destPort, err := strconv.Atoi(destPortStr)
	if err != nil {
		t.Fatalf("SPARK_E2E_PROBE_TARGET 端口无效：%v", err)
	}

	st, err := svc.AddSystemForward(keyID, types.SystemForwardRequest{
		Proto: "tcp", SrcPort: srcPort, DestIP: destHost, DestPort: destPort, Note: "e2e-probe",
	})
	if err != nil {
		t.Fatalf("创建转发（连通性验证）失败：%v", err)
	}
	var ruleID string
	for _, r := range st.Rules {
		if r.SrcPort == srcPort {
			ruleID = r.ID
		}
	}
	if ruleID == "" {
		t.Fatalf("未找到刚创建的转发规则：%+v", st.Rules)
	}
	defer func() {
		if _, err := svc.RemoveSystemForward(keyID, ruleID); err != nil {
			t.Errorf("清理连通性验证规则失败：%v", err)
		}
	}()

	addr := net.JoinHostPort(dialHost, strconv.Itoa(srcPort))
	conn, err := net.DialTimeout("tcp", addr, 12*time.Second)
	if err != nil {
		// 区分「规则没生效」和「云安全组/上游没放行」，后者不是应用的问题
		natOut, _, _ := run(fmt.Sprintf("iptables -w -t nat -L PREROUTING -n -v 2>/dev/null | grep -E '%d|DNAT' | head -5", srcPort))
		t.Fatalf("转发端口 %s 连不上，DNAT 可能未生效：%v\nnat 表计数：\n%s", addr, err, natOut)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	n, _ := conn.Read(buf)
	_ = conn.Close()
	banner := strings.TrimSpace(string(buf[:n]))
	t.Logf("连通性验证通过：%s → %s 已连通，对端返回 %q", addr, probe, banner)
}

// assertBackendHasRule 按后端检查内核/防火墙里的真实规则。
func assertBackendHasRule(t *testing.T, run fwRun, backend string, srcPort int, want bool) {
	t.Helper()
	port := strconv.Itoa(srcPort)
	switch backend {
	case fwBackendIPTables:
		out, _, _ := run("iptables -w -t nat -S; iptables -w -t filter -S FORWARD; iptables -w -t filter -S SPARK_FWD")
		if !want {
			if strings.Contains(out, port) {
				t.Errorf("删除后内核里仍有端口 %s 的规则：\n%s", port, out)
			}
			return
		}
		for _, w := range []string{
			fmt.Sprintf("--dport %s", port),
			"--to-destination 192.0.2.123:8443",
			"MASQUERADE", "-j ACCEPT", "-j SPARK_PRE", "-j SPARK_FWD",
		} {
			if !strings.Contains(out, w) {
				t.Errorf("内核里缺少 %q：\n%s", w, out)
			}
		}
	case fwBackendNFTables:
		out, _, _ := run("nft -a list table ip " + fwNATTable)
		has := strings.Contains(out, "dport "+port) && strings.Contains(out, "masquerade")
		if want && !has {
			t.Errorf("nftables 里缺少转发规则：\n%s", out)
		}
		if !want && strings.Contains(out, "dport "+port) {
			t.Errorf("删除后 nftables 里仍有该规则：\n%s", out)
		}
	case fwBackendFirewalld:
		out, _, _ := run("firewall-cmd --permanent --list-forward-ports; echo '@@'; firewall-cmd --list-forward-ports")
		has := strings.Contains(out, fmt.Sprintf("port=%s:", port))
		if want && !has {
			t.Errorf("firewalld 里缺少 forward-port：\n%s", out)
		}
		if !want && has {
			t.Errorf("删除后 firewalld 里仍有该规则：\n%s", out)
		}
		if want {
			if q, _, _ := run("firewall-cmd --query-masquerade && echo masq-on"); !strings.Contains(q, "masq-on") {
				t.Errorf("firewalld 未开启 masquerade，转发不会生效：\n%s", q)
			}
		}
	}
}

// assertOwnStructuresGone 确认应用自建的链 / 表在规则删光后也被清理掉了。
func assertOwnStructuresGone(t *testing.T, run fwRun, backend string) {
	t.Helper()
	switch backend {
	case fwBackendIPTables:
		out, _, _ := run("iptables -w -t nat -S; iptables -w -t filter -S")
		for _, w := range []string{fwIPTChainPre, fwIPTChainPost, fwIPTChainFwd} {
			if strings.Contains(out, w) {
				t.Errorf("删光规则后仍残留 %s：\n%s", w, out)
			}
		}
	case fwBackendNFTables:
		out, _, _ := run("nft list tables")
		for _, w := range []string{fwNATTable, fwFilterTable} {
			if strings.Contains(out, w) {
				t.Errorf("删光规则后仍残留 nftables 表 %s：\n%s", w, out)
			}
		}
	}
}

// pickFreePort 找一个内核确认空闲的临时端口，避免碰到线上服务。
func pickFreePort(t *testing.T, run fwRun) int {
	t.Helper()
	out, _, _ := run("(ss -Htuln 2>/dev/null || netstat -tuln 2>/dev/null)")
	for p := 39997; p < 40060; p++ {
		if strings.Contains(out, fmt.Sprintf(":%d", p)) {
			continue
		}
		return p
	}
	t.Skip("找不到空闲的高位端口")
	return 0
}

// sshRunner 用持久 SSH 连接实现 fwRun。
func sshRunner(client *xssh.Client) fwRun {
	return func(command string) (string, int, error) {
		sess, err := client.NewSession()
		if err != nil {
			return "", -1, err
		}
		defer sess.Close()
		out, err := sess.CombinedOutput(command)
		if err != nil {
			var ee *xssh.ExitError
			if errors.As(err, &ee) {
				return string(out), ee.ExitStatus(), nil
			}
			return string(out), -1, err
		}
		return string(out), 0, nil
	}
}

// dialE2EHost 连接用户指定的真实主机。
func dialE2EHost(t *testing.T, host string) *xssh.Client {
	t.Helper()
	user := envOr("SPARK_E2E_USER", "root")
	port := 22
	if p, err := strconv.Atoi(os.Getenv("SPARK_E2E_PORT")); err == nil && p > 0 {
		port = p
	}
	keyPath := os.Getenv("SPARK_E2E_KEY")
	if keyPath == "" {
		t.Skip("未设置 SPARK_E2E_KEY（私钥路径）")
	}
	if strings.HasPrefix(keyPath, "~/") {
		home, _ := os.UserHomeDir()
		keyPath = home + keyPath[1:]
	}
	pem, err := os.ReadFile(keyPath)
	if err != nil {
		t.Skipf("读取私钥失败：%v", err)
	}
	signer, err := xssh.ParsePrivateKey(pem)
	if err != nil {
		t.Skipf("解析私钥失败：%v", err)
	}
	cfg := &xssh.ClientConfig{
		User:            user,
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(signer)},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	client, err := xssh.Dial("tcp", host+":"+strconv.Itoa(port), cfg)
	if err != nil {
		t.Skipf("SSH 连接 %s 失败：%v", host, err)
	}
	return client
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// dialWSLSSHD 在 WSL 里临时起一个 sshd 供测试使用。
func dialWSLSSHD(t *testing.T) (*xssh.Client, func()) {
	t.Helper()
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("未找到 wsl.exe")
	}
	const (
		port = 2222
		mark = "spark-e2e"
	)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	authLine := strings.TrimSpace(string(xssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + mark
	install := fmt.Sprintf(
		"mkdir -p /root/.ssh /run/sshd && chmod 700 /root/.ssh && "+
			"grep -v %q /root/.ssh/authorized_keys > /tmp/ak 2>/dev/null; echo %q >> /tmp/ak; "+
			"mv /tmp/ak /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys && "+
			"(ss -tln | grep -q ':%d ' || /usr/sbin/sshd -p %d -o PermitRootLogin=prohibit-password -o PasswordAuthentication=no) && "+
			"sleep 1; ss -tln | grep ':%d ' | head -1",
		mark, authLine, port, port, port)
	out, err := exec.Command("wsl.exe", "-u", "root", "--", "bash", "-c", install).CombinedOutput()
	if err != nil || !strings.Contains(string(out), fmt.Sprintf(":%d", port)) {
		t.Skipf("无法在 WSL 中准备 sshd：%v\n%s", err, out)
	}
	cleanup := func() {
		exec.Command("wsl.exe", "-u", "root", "--", "bash", "-c",
			fmt.Sprintf("grep -v %q /root/.ssh/authorized_keys > /tmp/ak 2>/dev/null; mv /tmp/ak /root/.ssh/authorized_keys 2>/dev/null; "+
				"pkill -f 'sshd -p %d' 2>/dev/null; true", mark, port)).Run()
	}

	cfg := &xssh.ClientConfig{
		User:            "root",
		Auth:            []xssh.AuthMethod{xssh.PublicKeys(signer)},
		HostKeyCallback: xssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := xssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), cfg)
	if err != nil {
		cleanup()
		t.Skipf("SSH 连接失败：%v", err)
	}
	return client, cleanup
}
