package terminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spark/app/service/types"
)

// ---------------------------------------------------------------------------
// 解析器单元测试
// ---------------------------------------------------------------------------

func TestParseIPTablesRules(t *testing.T) {
	out := `-P PREROUTING ACCEPT
-P INPUT ACCEPT
-P POSTROUTING ACCEPT
-N SPARK_PRE
-N SPARK_POST
-N DOCKER
-A PREROUTING -j SPARK_PRE
-A PREROUTING -m addrtype --dst-type LOCAL -j DOCKER
-A POSTROUTING -s 172.17.0.0/16 ! -o docker0 -j MASQUERADE
-A POSTROUTING -j SPARK_POST
-A SPARK_PRE -p tcp -m tcp --dport 18080 -m comment --comment spark-fwd-0123456789ab -j DNAT --to-destination 10.1.2.3:8080
-A SPARK_PRE -p udp -m udp --dport 15353 -j DNAT --to-destination 8.8.8.8:53
-A SPARK_POST -d 10.1.2.3/32 -p tcp -m tcp --dport 8080 -m comment --comment spark-fwd-0123456789ab -j MASQUERADE
-A DOCKER ! -i br-abc -p tcp -m tcp --dport 3306 -j DNAT --to-destination 172.18.0.3:3306
`
	rules := parseIPTablesRules(out)
	if len(rules) != 3 {
		t.Fatalf("期望解析出 3 条转发规则，实际 %d：%+v", len(rules), rules)
	}

	managed := rules[0]
	if managed.fwd.ID != "spark-fwd-0123456789ab" || !managed.fwd.Managed {
		t.Errorf("第一条应为本应用创建的规则（带注释）：%+v", managed.fwd)
	}
	if managed.fwd.Proto != "tcp" || managed.fwd.SrcPort != 18080 ||
		managed.fwd.DestIP != "10.1.2.3" || managed.fwd.DestPort != 8080 {
		t.Errorf("第一条规则字段解析错误：%+v", managed.fwd)
	}
	if managed.chain != fwIPTChainPre || managed.table != "nat" || managed.pos != 1 {
		t.Errorf("第一条规则定位信息错误：chain=%s table=%s pos=%d", managed.chain, managed.table, managed.pos)
	}

	// 没有任何注释的系统规则 → 未托管，且规则序号要算准（SPARK_PRE 里它是第 2 条）
	unmanaged := rules[1]
	if unmanaged.fwd.Managed {
		t.Errorf("无注释的规则不应标记为本应用创建：%+v", unmanaged.fwd)
	}
	if unmanaged.chain != fwIPTChainPre || unmanaged.pos != 2 {
		t.Errorf("无注释规则的序号应为 2，实际 %d", unmanaged.pos)
	}
	if unmanaged.fwd.DestIP != "8.8.8.8" || unmanaged.fwd.DestPort != 53 {
		t.Errorf("无注释规则字段解析错误：%+v", unmanaged.fwd)
	}

	// MASQUERADE 是配套规则，不应单独成条
	for _, r := range rules {
		if r.chain == fwIPTChainPost {
			t.Errorf("MASQUERADE 规则不应出现在转发列表里：%+v", r.fwd)
		}
	}
	// Docker 的 DNAT 属于系统已有规则，ID 稳定
	docker := rules[2]
	if docker.fwd.Managed || !strings.HasPrefix(docker.fwd.ID, "sys-") {
		t.Errorf("Docker 规则应为系统已有规则：%+v", docker.fwd)
	}
	if docker.fwd.SrcPort != 3306 || docker.fwd.DestPort != 3306 {
		t.Errorf("Docker 规则字段解析错误：%+v", docker.fwd)
	}
}

func TestParseNFTRules(t *testing.T) {
	out := `table ip spark_nat { # handle 8
	chain prerouting { # handle 1
		type nat hook prerouting priority dstnat - 10; policy accept;
		tcp dport 18080 dnat to 10.1.2.3:8080 comment "spark-fwd-0123456789ab" # handle 3
		udp dport 15353 dnat to 8.8.8.8:53 # handle 4
	}

	chain postrouting { # handle 2
		type nat hook postrouting priority srcnat + 10; policy accept;
		ip daddr 10.1.2.3 tcp dport 8080 masquerade comment "spark-fwd-0123456789ab" # handle 5
	}
}
@@END@@
`
	rules := parseNFTRules(out)
	if len(rules) != 2 {
		t.Fatalf("期望解析出 2 条转发规则，实际 %d：%+v", len(rules), rules)
	}
	first := rules[0]
	if first.fwd.ID != "spark-fwd-0123456789ab" || !first.fwd.Managed {
		t.Errorf("第一条应为本应用创建的规则：%+v", first.fwd)
	}
	if first.fwd.Proto != "tcp" || first.fwd.SrcPort != 18080 ||
		first.fwd.DestIP != "10.1.2.3" || first.fwd.DestPort != 8080 {
		t.Errorf("第一条规则字段解析错误：%+v", first.fwd)
	}
	if first.table != "ip spark_nat" || first.chain != "prerouting" || first.handle != 3 {
		t.Errorf("第一条规则定位信息错误：%+v", first)
	}
	if rules[1].fwd.Managed || rules[1].handle != 4 {
		t.Errorf("第二条应为系统已有规则且 handle=4：%+v", rules[1])
	}
}

func TestParseFirewalldForwardPorts(t *testing.T) {
	out := "port=18080:proto=tcp:toport=8080:toaddr=10.1.2.3 port=15353:proto=udp:toport=53:toaddr=8.8.8.8\n"
	rules := parseFirewalldForwardPorts(out, "public")
	if len(rules) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(rules))
	}
	if got := firewalldSpec(rules[0].fwd); got != "port=18080:proto=tcp:toport=8080:toaddr=10.1.2.3" {
		t.Errorf("规格串还原错误：%s", got)
	}
	if got := firewalldSpec(rules[1].fwd); got != "port=15353:proto=udp:toport=53:toaddr=8.8.8.8" {
		t.Errorf("规格串还原错误：%s", got)
	}
	// 同端口转发不需要 toport
	same := types.SystemForward{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "10.0.0.9"}
	if got := firewalldSpec(same); got != "port=80:proto=tcp:toaddr=10.0.0.9" {
		t.Errorf("同端口规格串错误：%s", got)
	}
	// 转发到本机其它端口
	local := types.SystemForward{Proto: "tcp", SrcPort: 80, DestPort: 8080}
	if got := firewalldSpec(local); got != "port=80:proto=tcp:toport=8080" {
		t.Errorf("本机端口规格串错误：%s", got)
	}
}

func TestParseFirewalldRichRules(t *testing.T) {
	out := `rule family="ipv4" forward port port="18080" protocol="tcp" to-port="8080" to-addr="10.1.2.3"
rule family="ipv4" source address="1.2.3.4/32" forward port port="53" protocol="udp" to-port="53" to-addr="8.8.8.8"
rule family="ipv4" masquerade
`
	rules := parseFirewalldRichRules(out, "public")
	if len(rules) != 2 {
		t.Fatalf("期望 2 条富规则，实际 %d", len(rules))
	}
	f := rules[0].fwd
	if f.SrcPort != 18080 || f.DestPort != 8080 || f.DestIP != "10.1.2.3" || f.Proto != "tcp" {
		t.Errorf("富规则解析错误：%+v", f)
	}
	if rules[0].spec == "" {
		t.Error("富规则应保留原始文本用于精确删除")
	}
}

func TestValidateSystemForward(t *testing.T) {
	cases := []struct {
		name string
		req  types.SystemForwardRequest
		want string
	}{
		{"协议非法", types.SystemForwardRequest{Proto: "sctp", SrcPort: 80, DestPort: 80, DestIP: "10.0.0.1"}, "协议"},
		{"外部端口越界", types.SystemForwardRequest{Proto: "tcp", SrcPort: 0, DestPort: 80, DestIP: "10.0.0.1"}, "外部端口"},
		{"目标端口越界", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 70000, DestIP: "10.0.0.1"}, "目标端口"},
		{"目标不是 IP", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "example.com"}, "IPv4"},
		{"目标 IPv6", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "::1"}, "IPv4"},
		{"回环目标", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "127.0.0.1"}, "回环"},
		{"通配目标", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "0.0.0.0"}, "0.0.0.0"},
		{"区域名非法", types.SystemForwardRequest{Proto: "tcp", SrcPort: 80, DestPort: 80, DestIP: "10.0.0.1", Zone: "public;rm -rf /"}, "区域名"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSystemForward(&c.req)
			if err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("期望错误包含 %q，实际 %q", c.want, err.Error())
			}
		})
	}

	ok := types.SystemForwardRequest{Proto: "", SrcPort: 80, DestPort: 8080, DestIP: " 10.0.0.5 "}
	if err := validateSystemForward(&ok); err != nil {
		t.Fatalf("合法请求不应失败：%v", err)
	}
	if ok.Proto != "tcp" || ok.DestIP != "10.0.0.5" {
		t.Errorf("请求未规范化：%+v", ok)
	}
}

func TestSidecarRoundTrip(t *testing.T) {
	env := &fwEnv{}
	sc := parseSidecar(env.Sidecar)
	sc.Rules = append(sc.Rules, fwNote{
		ID: "spark-fwd-abc", Backend: fwBackendFirewalld, Proto: "tcp",
		SrcPort: 18080, DestIP: "10.1.2.3", DestPort: 8080, Zone: "public", Note: "测试",
	})
	env.Sidecar = mustJSON(sc)

	rules := []fwRule{{fwd: types.SystemForward{
		Backend: fwBackendFirewalld, Proto: "tcp", SrcPort: 18080, DestIP: "10.1.2.3", DestPort: 8080, Zone: "public",
	}}}
	applySidecar(rules, env)
	if !rules[0].fwd.Managed || rules[0].fwd.ID != "spark-fwd-abc" || rules[0].fwd.Note != "测试" {
		t.Fatalf("firewalld 规则的来源/备注未从清单合并：%+v", rules[0].fwd)
	}

	// 非默认区域：规则自身的区域要参与匹配，否则会误判成「系统已有」
	other := []fwRule{{fwd: types.SystemForward{
		Backend: fwBackendFirewalld, Proto: "tcp", SrcPort: 18080, DestIP: "10.1.2.3", DestPort: 8080, Zone: "dmz",
	}}}
	applySidecar(other, env)
	if other[0].fwd.Managed {
		t.Fatalf("区域不同的规则不应被判定为本应用创建：%+v", other[0].fwd)
	}

	pruneSidecar(env, "spark-fwd-abc")
	if len(parseSidecar(env.Sidecar).Rules) != 0 {
		t.Fatal("清单项未被移除")
	}
	// 清空后写入片段应删除清单文件
	if !strings.Contains(sidecarWriteSnippet(parseSidecar(env.Sidecar)), "rm -f") {
		t.Error("空清单应删除远端文件")
	}
}

// TestCoexistWarning 覆盖「ufw 与 firewalld 同时运行会吃掉转发流量」的告警
// （该现象已在真实服务器上复现：ufw 的 FORWARD 拒绝规则优先级比 firewalld 更靠前）。
func TestCoexistWarning(t *testing.T) {
	cases := []struct {
		name string
		env  fwEnv
		want bool
	}{
		{"ufw + firewalld", fwEnv{UFWActive: true, Backend: fwBackendFirewalld}, true},
		{"只有 firewalld", fwEnv{Backend: fwBackendFirewalld}, false},
		{"ufw + iptables（插在 FORWARD 首位，不受影响）", fwEnv{UFWActive: true, Backend: fwBackendIPTables}, false},
		{"ufw + nftables", fwEnv{UFWActive: true, Backend: fwBackendNFTables}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fwCoexistWarning(&c.env)
			if (got != "") != c.want {
				t.Fatalf("期望告警=%v，实际 %q", c.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 解析器健壮性：不允许把任意文本当成规则
// ---------------------------------------------------------------------------

func TestParsersIgnoreNoise(t *testing.T) {
	if got := parseIPTablesRules("iptables: Permission denied (you must be root)\n"); len(got) != 0 {
		t.Errorf("错误输出不应产生规则：%+v", got)
	}
	if got := parseNFTRules("Error: No such file or directory\n"); len(got) != 0 {
		t.Errorf("错误输出不应产生规则：%+v", got)
	}
	if got := parseFirewalldForwardPorts("Authorization failed.", "public"); len(got) != 0 {
		t.Errorf("错误输出不应产生规则：%+v", got)
	}
}

// ---------------------------------------------------------------------------
// WSL 集成测试：真实执行生成的脚本（iptables / nftables）
// ---------------------------------------------------------------------------

// wslRun 把脚本交给 WSL（root）执行，用作 fwRun 的真实实现。
//
// 注意：脚本先落盘成 .sh 再执行。直接把脚本当命令行参数传给 wsl.exe 会因为
// Windows→Linux 的参数转义把反斜杠（`\n`、`case x in *a\ b*`）弄坏，
// 那是测试通道的问题，不是被测脚本的问题（真实链路是 SSH，没有这一层）。
func wslRun(_ context.Context, command string) (string, int, error) {
	dir, err := os.MkdirTemp("", "spark-wslrun")
	if err != nil {
		return "", -1, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "cmd.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+command+"\n"), 0o644); err != nil {
		return "", -1, err
	}
	cmd := exec.Command("wsl.exe", "-u", "root", "--", "bash", toWSLPath(file))
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			err = nil
		}
	}
	return string(out), code, nil
}

// toWSLPath 把 C:\Users\x\f.sh 转成 /mnt/c/Users/x/f.sh。
func toWSLPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) > 1 && p[1] == ':' {
		p = "/mnt/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

func requireWSL(t *testing.T) fwRun {
	t.Helper()
	if os.Getenv("SPARK_SKIP_WSL_TEST") != "" {
		t.Skip("SPARK_SKIP_WSL_TEST 已设置")
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("未找到 wsl.exe，跳过集成测试")
	}
	probe := exec.Command("wsl.exe", "-u", "root", "--", "true")
	if err := probe.Run(); err != nil {
		t.Skipf("WSL 不可用（%v），跳过集成测试", err)
	}
	run := fwRun(func(command string) (string, int, error) { return wslRun(context.Background(), command) })
	if out, _, _ := run("iptables -w -t nat -S >/dev/null 2>&1 && echo ok"); !strings.Contains(out, "ok") {
		t.Skip("WSL 内没有可用的 iptables，跳过集成测试")
	}
	return run
}

// isolateFWPaths 把远端写文件的位置改到临时目录，避免污染真实系统配置。
func isolateFWPaths(t *testing.T) {
	t.Helper()
	old := []string{fwSidecarPath, fwNftConfPath, fwIPTablesSavePath, fwSysctlFile}
	fwSidecarPath = "/tmp/spark-fw-test/portforward.json"
	fwNftConfPath = "/tmp/spark-fw-test/nftables.conf"
	fwIPTablesSavePath = "/tmp/spark-fw-test/rules.v4"
	fwSysctlFile = "/tmp/spark-fw-test/99-spark-forward.conf"
	t.Cleanup(func() {
		fwSidecarPath, fwNftConfPath, fwIPTablesSavePath, fwSysctlFile = old[0], old[1], old[2], old[3]
	})
}

const fwCleanupScript = `iptables -w -t nat -D PREROUTING -j SPARK_PRE 2>/dev/null
iptables -w -t nat -D POSTROUTING -j SPARK_POST 2>/dev/null
iptables -w -t filter -D FORWARD -j SPARK_FWD 2>/dev/null
iptables -w -t nat -F SPARK_PRE 2>/dev/null; iptables -w -t nat -X SPARK_PRE 2>/dev/null
iptables -w -t nat -F SPARK_POST 2>/dev/null; iptables -w -t nat -X SPARK_POST 2>/dev/null
iptables -w -t filter -F SPARK_FWD 2>/dev/null; iptables -w -t filter -X SPARK_FWD 2>/dev/null
nft delete table ip spark_nat 2>/dev/null
nft delete table inet spark_filter 2>/dev/null
rm -rf /tmp/spark-fw-test
echo cleaned`

func TestWSLSystemForwardIPTables(t *testing.T) {
	run := requireWSL(t)
	isolateFWPaths(t)
	defer func() {
		out, _, _ := run(fwCleanupScript)
		if !strings.Contains(out, "cleaned") {
			t.Errorf("清理失败：%s", out)
		}
	}()

	env, err := probeFirewallEnv(run)
	if err != nil {
		t.Fatalf("探测失败：%v", err)
	}
	if !env.HasIPTables {
		t.Skip("WSL 无 iptables")
	}
	env.Backend = fwBackendIPTables

	req := types.SystemForwardRequest{
		Proto: "tcp", SrcPort: 18999, DestIP: "10.11.12.13", DestPort: 8080, Note: "集成测试",
	}
	if err := addSystemForwardRule(run, env, req); err != nil {
		t.Fatalf("创建 iptables 转发失败：%v", err)
	}

	// 真实内核里应当出现 DNAT / MASQUERADE / ACCEPT 三条配套规则，
	// 以及挂到内置链上的跳转规则
	out, _, _ := run("iptables -w -t nat -S SPARK_PRE; iptables -w -t nat -S SPARK_POST; iptables -w -t filter -S SPARK_FWD; " +
		"iptables -w -t nat -S PREROUTING; iptables -w -t nat -S POSTROUTING; iptables -w -t filter -S FORWARD")
	for _, want := range []string{
		"--dport 18999",
		"--to-destination 10.11.12.13:8080",
		"MASQUERADE",
		"-j ACCEPT",
		"-j SPARK_PRE",
		"-j SPARK_POST",
		"-j SPARK_FWD",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("内核规则缺少 %q：\n%s", want, out)
		}
	}
	if !strings.Contains(out, fwCommentPrefix) {
		t.Errorf("内核规则缺少本应用标记：\n%s", out)
	}

	// 重新探测（重新读取远端清单）后应能识别出这条规则及其备注
	env2, err := probeFirewallEnv(run)
	if err != nil {
		t.Fatalf("二次探测失败：%v", err)
	}
	env2.Backend = fwBackendIPTables
	rules, err := listSystemForwardRules(run, env2, "")
	if err != nil {
		t.Fatalf("列出规则失败：%v", err)
	}
	var found *fwRule
	for i := range rules {
		if rules[i].fwd.SrcPort == 18999 {
			found = &rules[i]
		}
	}
	if found == nil {
		t.Fatalf("未列出刚创建的规则：%+v", rules)
	}
	if !found.fwd.Managed || found.fwd.Note != "集成测试" || found.fwd.DestIP != "10.11.12.13" {
		t.Fatalf("规则信息不完整：%+v", found.fwd)
	}

	// 持久化文件应已写出
	if out, _, _ := run("test -s " + fwIPTablesSavePath + " && echo yes"); !strings.Contains(out, "yes") {
		t.Errorf("iptables 规则未持久化到 %s", fwIPTablesSavePath)
	}

	if err := removeSystemForwardRule(run, env2, found.fwd.ID); err != nil {
		t.Fatalf("删除规则失败：%v", err)
	}
	out, _, _ = run("iptables -w -t nat -S SPARK_PRE; iptables -w -t filter -S SPARK_FWD")
	if strings.Contains(out, "18999") {
		t.Errorf("规则未删除干净：\n%s", out)
	}
	// 链和跳转保留（其它规则可能还在用），但不应再有本应用的规则
	if out, _, _ := run("test -f " + fwSidecarPath + " && echo yes"); strings.Contains(out, "yes") {
		t.Errorf("清单文件应已清空删除")
	}
}

func TestWSLSystemForwardNFTables(t *testing.T) {
	run := requireWSL(t)
	isolateFWPaths(t)
	defer func() { run(fwCleanupScript) }()

	if out, _, _ := run("nft list tables >/dev/null 2>&1 && echo ok"); !strings.Contains(out, "ok") {
		t.Skip("WSL 无 nftables")
	}
	env, err := probeFirewallEnv(run)
	if err != nil {
		t.Fatalf("探测失败：%v", err)
	}
	env.Backend = fwBackendNFTables

	req := types.SystemForwardRequest{
		Proto: "udp", SrcPort: 18998, DestIP: "10.11.12.14", DestPort: 5353, Note: "nft 集成测试",
	}
	if err := addSystemForwardRule(run, env, req); err != nil {
		t.Fatalf("创建 nftables 转发失败：%v", err)
	}

	out, _, _ := run("nft -a list table ip " + fwNATTable + "; nft -a list table inet " + fwFilterTable)
	if !strings.Contains(out, "dnat to 10.11.12.14:5353") {
		t.Errorf("nftables 缺少 DNAT 规则：\n%s", out)
	}
	if !strings.Contains(out, "masquerade") || !strings.Contains(out, "accept") {
		t.Errorf("nftables 缺少 MASQUERADE / ACCEPT 规则：\n%s", out)
	}

	env2, err := probeFirewallEnv(run)
	if err != nil {
		t.Fatalf("二次探测失败：%v", err)
	}
	env2.Backend = fwBackendNFTables
	rules, err := listSystemForwardRules(run, env2, "")
	if err != nil {
		t.Fatalf("列出规则失败：%v", err)
	}
	var found *fwRule
	for i := range rules {
		if rules[i].fwd.SrcPort == 18998 {
			found = &rules[i]
		}
	}
	if found == nil {
		t.Fatalf("未列出刚创建的规则：%+v", rules)
	}
	if found.fwd.Proto != "udp" || found.fwd.DestPort != 5353 || found.handle == 0 || !found.fwd.Managed {
		t.Fatalf("规则信息不完整：%+v", found)
	}

	// 持久化：/etc/nftables.conf 的 spark 区块
	if out, _, _ := run("grep -c 'spark port-forward' " + fwNftConfPath); !strings.Contains(strings.TrimSpace(out), "2") {
		t.Errorf("nftables 配置未写入标记区块：%s", out)
	}

	if err := removeSystemForwardRule(run, env2, found.fwd.ID); err != nil {
		t.Fatalf("删除规则失败：%v", err)
	}
	out, _, _ = run("nft list table ip " + fwNATTable)
	if strings.Contains(out, "18998") {
		t.Errorf("nftables 规则未删除干净：\n%s", out)
	}
	// 删光规则后自建的表应整体撤掉
	if out, _, _ := run("nft list tables"); strings.Contains(out, fwNATTable) || strings.Contains(out, fwFilterTable) {
		t.Errorf("删光规则后仍残留 spark 表：\n%s", out)
	}
}

// TestWSLNoPrivilegeMessage 确认非 root 且无 sudo 时给出可读提示而不是崩溃。
func TestProbeWithoutPrivilege(t *testing.T) {
	if os.Getenv("SPARK_SKIP_WSL_TEST") != "" {
		t.Skip("SPARK_SKIP_WSL_TEST 已设置")
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("未找到 wsl.exe")
	}
	run := fwRun(func(command string) (string, int, error) { return wslRun(context.Background(), command) })
	// 以 nobody 身份执行探测脚本，检查不会 panic 且能解析出信息
	out, _, err := run("su -s /bin/sh nobody -c " + shQuote("sh -c "+shQuote(fwProbeScript())))
	if err != nil {
		t.Skipf("无法切换到 nobody：%v", err)
	}
	env, err := probeFirewallEnv(func(string) (string, int, error) { return out, 0, nil })
	if err != nil {
		t.Fatalf("探测解析失败：%v", err)
	}
	if env.UID == 0 {
		t.Skipf("nobody 的 uid 为 0，当前环境无可用的非特权用户，跳过")
	}
	if !env.HasIPTables || !env.HasNFT {
		t.Errorf("命令存在性判断应不受权限影响：%+v", env)
	}
	if env.privileged() {
		t.Errorf("nobody 不应被判定为有权限")
	}
}

var _ = time.Second