// 系统转发：在远端服务器上通过防火墙（firewalld / iptables / nftables）配置
// 系统级 NAT 端口转发。
//
// 与「会话转发」（ssh -L / -R / -D）的区别：
//   - 会话转发由本进程持有 SSH 连接，会话断开即失效；
//   - 系统转发把 DNAT + MASQUERADE + FORWARD 规则写进远端防火墙并持久化，
//     不需要本进程在线，重启服务器后依然生效。
//
// 三种后端的落地方式：
//   - firewalld：--add-forward-port（等价于 forward 富规则）+ --add-masquerade，
//     runtime 与 permanent 双写；富规则本身不支持注释，来源记录在同目录的
//     远端清单文件里；
//   - iptables：自建 SPARK_PRE / SPARK_POST / SPARK_FWD 三条链，规则带
//     -m comment --comment spark-fwd-xxx 标记，用 iptables-save 持久化；
//   - nftables：自建 ip spark_nat / inet spark_filter 表，规则带 comment 标记，
//     持久化到 /etc/nftables.conf 的 spark 区块。
//
// 本应用只保证能正确撤销自己创建的规则；面板里同时会把系统里已有的转发规则
// 列出来（标记为「系统已有」），删除时按精确规则删除。
package terminal

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"spark/app/service/types"
)

// fwBackend* 是受支持的防火墙后端标识。
const (
	fwBackendFirewalld = "firewalld"
	fwBackendIPTables  = "iptables"
	fwBackendNFTables  = "nftables"
	fwBackendNone      = "none"
)

const (
	fwCommentPrefix = "spark-fwd-"   // 规则标记（iptables / nftables 注释，兼作规则 ID）
	fwNATTable      = "spark_nat"    // nftables NAT 表
	fwFilterTable   = "spark_filter" // nftables filter 表（放行 FORWARD）
	fwIPTChainPre   = "SPARK_PRE"    // iptables nat/PREROUTING：DNAT
	fwIPTChainPost  = "SPARK_POST"   // iptables nat/POSTROUTING：MASQUERADE
	fwIPTChainFwd   = "SPARK_FWD"    // iptables filter/FORWARD：ACCEPT
)

// 以下是远端写文件的位置，做成变量便于测试时重定向到临时目录。
var (
	fwSidecarPath      = "/etc/spark/portforward.json" // 远端规则清单（备注 / 来源）
	fwNftConfPath      = "/etc/nftables.conf"          // nftables 持久化配置
	fwIPTablesSavePath = "/etc/iptables/rules.v4"      // iptables-save 输出
	fwSysctlFile       = "/etc/sysctl.d/99-spark-forward.conf"
)

// fwRun 执行一条远端命令（command 已包含 sh -c / sudo 包装），
// 返回合并输出、退出码与传输层错误。
type fwRun func(command string) (string, int, error)

// fwEnv 是一次远端防火墙环境探测的结果。
type fwEnv struct {
	Backend           string
	UID               int
	SudoOK            bool
	FirewalldRunning  bool
	DefaultZone       string
	HasIPTables       bool
	NATOK             bool
	HasNFT            bool
	NFTOK             bool
	IPForward         bool
	UFWActive         bool
	NetfilterPersist  bool
	IptablesSave      bool
	Systemctl         bool
	EtcIptables       bool
	SysconfigIPtables bool
	NftablesConf      bool
	Sidecar           string // /etc/spark/portforward.json 原文（可能为空）
}

// fwRule 是内部规则表示，附带删除所需的定位信息。
type fwRule struct {
	fwd    types.SystemForward
	table  string // iptables/nftables 表名（nft 含族，如 "ip spark_nat"）
	chain  string // 链名
	pos    int    // iptables 规则序号（1 起）
	handle int    // nftables 规则 handle
	spec   string // firewalld --remove-forward-port 规格
}

// fwSidecar 是本应用在远端维护的清单文件（备注 / 创建时间）。
type fwSidecar struct {
	Version int      `json:"version"`
	Rules   []fwNote `json:"rules"`
}

type fwNote struct {
	ID        string `json:"id"`
	Backend   string `json:"backend"`
	Proto     string `json:"proto"`
	SrcPort   int    `json:"srcPort"`
	DestIP    string `json:"destIp"`
	DestPort  int    `json:"destPort"`
	Zone      string `json:"zone,omitempty"`
	Note      string `json:"note,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// ---------------------------------------------------------------------------
// 环境探测
// ---------------------------------------------------------------------------

// fwProbeScript 生成探测脚本（路径用变量，便于测试重定向）。
func fwProbeScript() string {
	return `exec 2>&1
SUDO=
if [ "$(id -u)" != "0" ]; then
  if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then SUDO="sudo -n"; fi
fi
echo "@@UID@@"
id -u 2>/dev/null
echo "@@SUDO@@"
if [ -n "$SUDO" ]; then echo yes; fi
echo "@@FIREWALLD@@"
command -v firewall-cmd 2>/dev/null
firewall-cmd --state 2>/dev/null
echo "@@ZONE@@"
firewall-cmd --get-default-zone 2>/dev/null
echo "@@IPTABLES@@"
command -v iptables 2>/dev/null
echo "@@NATOK@@"
if $SUDO iptables -w -t nat -S >/dev/null 2>&1; then echo yes; fi
echo "@@NFT@@"
command -v nft 2>/dev/null
echo "@@NFTOK@@"
if $SUDO nft list tables >/dev/null 2>&1; then echo yes; fi
echo "@@PERSIST@@"
command -v netfilter-persistent 2>/dev/null
command -v iptables-save 2>/dev/null
command -v systemctl 2>/dev/null
if [ -d /etc/iptables ]; then echo etc-iptables; fi
if [ -f /etc/sysconfig/iptables ]; then echo sysconfig-iptables; fi
if [ -f /etc/nftables.conf ]; then echo nftables-conf; fi
echo "@@IPFORWARD@@"
cat /proc/sys/net/ipv4/ip_forward 2>/dev/null
echo "@@UFW@@"
$SUDO ufw status 2>/dev/null | head -1
echo "@@SIDECAR@@"
cat ` + shQuote(fwSidecarPath) + ` 2>/dev/null
echo ""
echo "@@END@@"
`
}

// probeFirewallEnv 探测远端防火墙后端与权限。探测脚本本身不需要 root，
// 因此即使返回非 0 退出码也继续解析已有信息。
func probeFirewallEnv(run fwRun) (*fwEnv, error) {
	out, _, err := run("sh -c " + shQuote(fwProbeScript()))
	if err != nil {
		return nil, err
	}
	sec := splitSections(out)
	env := &fwEnv{}
	env.UID = atoi(sectionFirst(sec["UID"]))
	env.SudoOK = sectionHas(sec["SUDO"], "yes")
	env.FirewalldRunning = sectionHas(sec["FIREWALLD"], "running")
	env.DefaultZone = sectionFirst(sec["ZONE"])
	env.HasIPTables = sectionFirst(sec["IPTABLES"]) != ""
	env.NATOK = sectionHas(sec["NATOK"], "yes")
	env.HasNFT = sectionFirst(sec["NFT"]) != ""
	env.NFTOK = sectionHas(sec["NFTOK"], "yes")
	env.NetfilterPersist = hasAnyLine(sec["PERSIST"], "netfilter-persistent")
	env.IptablesSave = hasAnyLine(sec["PERSIST"], "iptables-save")
	env.Systemctl = hasAnyLine(sec["PERSIST"], "systemctl")
	env.EtcIptables = sectionHas(sec["PERSIST"], "etc-iptables")
	env.SysconfigIPtables = sectionHas(sec["PERSIST"], "sysconfig-iptables")
	env.NftablesConf = sectionHas(sec["PERSIST"], "nftables-conf")
	env.IPForward = strings.TrimSpace(sectionFirst(sec["IPFORWARD"])) == "1"
	env.UFWActive = sectionHas(sec["UFW"], "Status: active")
	env.Sidecar = strings.TrimSpace(sec["SIDECAR"])
	env.Backend = env.decideBackend()
	return env, nil
}

// decideBackend 选择落地后端：firewalld 在跑就优先用它（否则直接写 iptables
// 会被 firewalld 覆盖）；其次 iptables（nft 后端系统上也有 iptables-nft 包装，
// 且持久化工具更成熟）；最后才是原生 nftables。
func (e *fwEnv) decideBackend() string {
	switch {
	case e.FirewalldRunning:
		return fwBackendFirewalld
	case e.HasIPTables && e.NATOK:
		return fwBackendIPTables
	case e.HasNFT && e.NFTOK:
		return fwBackendNFTables
	case e.HasIPTables:
		return fwBackendIPTables
	case e.HasNFT:
		return fwBackendNFTables
	default:
		return fwBackendNone
	}
}

func (e *fwEnv) privileged() bool { return e.UID == 0 || e.SudoOK }

func (e *fwEnv) privilegeName() string {
	switch {
	case e.UID == 0:
		return "root"
	case e.SudoOK:
		return "sudo"
	default:
		return "none"
	}
}

// wrap 用 root（或免密 sudo）执行脚本，脚本内部因此无需逐条加 sudo。
func (e *fwEnv) wrap(script string) string {
	if e.UID == 0 {
		return "sh -c " + shQuote(script)
	}
	return "sudo -n sh -c " + shQuote(script)
}

func (e *fwEnv) exec(run fwRun, script string) (string, int, error) {
	return run(e.wrap(script))
}

// ---------------------------------------------------------------------------
// 状态组装
// ---------------------------------------------------------------------------

func buildSystemForwardStatus(run fwRun, env *fwEnv) *types.SystemForwardStatus {
	st := &types.SystemForwardStatus{
		Backend:     env.Backend,
		BackendName: backendName(env.Backend),
		Privilege:   env.privilegeName(),
		IPForward:   env.IPForward,
		Zone:        env.DefaultZone,
		Persist:     env.persistHint(),
	}
	if env.Backend == fwBackendNone {
		switch {
		case env.HasIPTables || env.HasNFT:
			st.Message = "检测到防火墙工具但没有权限读取规则：请用 root 账号，或让该账号可免密 sudo"
		default:
			st.Message = "未检测到 firewalld / iptables / nftables，系统转发需要其中之一的防火墙后端"
		}
		return st
	}
	if !env.privileged() {
		st.Message = "需要 root 权限（或免密 sudo）才能创建系统转发规则"
		return st
	}
	st.Available = true

	rules, err := listSystemForwardRules(run, env, env.DefaultZone)
	if err != nil {
		st.Available = false
		st.Message = err.Error()
		return st
	}
	st.Rules = make([]types.SystemForward, 0, len(rules))
	for _, r := range rules {
		st.Rules = append(st.Rules, r.fwd)
		if r.fwd.Managed {
			st.Managed++
		}
	}
	st.Total = len(st.Rules)
	if w := fwCoexistWarning(env); w != "" {
		st.Message = w
	}
	return st
}

// fwCoexistWarning 返回「有别的防火墙会抢在前面把转发流量丢掉」的提示。
// 实测：ufw 与 firewalld 同时运行时，ufw 的 FORWARD 拒绝规则在 nft 里优先级更靠前，
// firewalld 的 ct status dnat accept 根本没机会执行，转发端口连不通。
func fwCoexistWarning(env *fwEnv) string {
	if env.UFWActive && env.Backend == fwBackendFirewalld {
		return "检测到 ufw 仍在运行：ufw 的转发拦截规则优先于 firewalld 生效，会把系统转发的流量丢掉。" +
			"请先执行 ufw disable（或将转发交给 ufw 管理）后再使用系统转发"
	}
	return ""
}

func backendName(b string) string {
	switch b {
	case fwBackendFirewalld:
		return "firewalld"
	case fwBackendIPTables:
		return "iptables"
	case fwBackendNFTables:
		return "nftables"
	default:
		return "未检测到"
	}
}

// persistHint 说明当前后端的持久化方式（面板底部提示用）。
func (e *fwEnv) persistHint() string {
	switch e.Backend {
	case fwBackendFirewalld:
		return "firewalld runtime + permanent 双写，重启后自动恢复"
	case fwBackendIPTables:
		switch {
		case e.NetfilterPersist:
			return "iptables 规则 + netfilter-persistent 保存，重启后自动恢复"
		case e.IptablesSave:
			if e.EtcIptables {
				return "iptables 规则已写入 /etc/iptables/rules.v4（需安装 iptables-persistent 才能在重启后恢复）"
			}
			return "iptables 规则 + iptables-save 写入 /etc/iptables/rules.v4，重启后自动恢复"
		default:
			return "未找到 iptables 持久化工具，规则重启后会丢失"
		}
	case fwBackendNFTables:
		return "nftables 规则写入 /etc/nftables.conf 的 spark 区块，重启后自动恢复"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// 规则列表
// ---------------------------------------------------------------------------

// listSystemForwardRules 列出当前后端上的转发规则，并把远端清单里的备注 /
// 来源合并进来。zone 为空时使用探测到的默认区域。
func listSystemForwardRules(run fwRun, env *fwEnv, zone string) ([]fwRule, error) {
	if zone == "" {
		zone = env.DefaultZone
	}
	var (
		rules []fwRule
		err   error
	)
	switch env.Backend {
	case fwBackendFirewalld:
		rules, err = listFirewalldRules(run, env, zone)
	case fwBackendIPTables:
		rules, err = listIPTablesRules(run, env)
	case fwBackendNFTables:
		rules, err = listNFTablesRules(run, env)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	applySidecar(rules, env)
	sortSystemForwards(rules)
	return rules, nil
}

func sortSystemForwards(rules []fwRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		a, b := rules[i].fwd, rules[j].fwd
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.SrcPort != b.SrcPort {
			return a.SrcPort < b.SrcPort
		}
		if a.DestIP != b.DestIP {
			return a.DestIP < b.DestIP
		}
		return a.DestPort < b.DestPort
	})
}

// --- iptables ---

const fwIPTListScript = `exec 2>&1
iptables -w -t nat -S 2>&1
echo "@@END@@"
`

func listIPTablesRules(run fwRun, env *fwEnv) ([]fwRule, error) {
	out, code, err := env.exec(run, fwIPTListScript)
	if err != nil {
		return nil, err
	}
	if code != 0 && strings.TrimSpace(out) == "" {
		return nil, errors.New("读取 iptables nat 表失败，请确认具备 root 权限")
	}
	return parseIPTablesRules(out), nil
}

// parseIPTablesRules 解析 `iptables -t nat -S` 输出，只保留 DNAT / REDIRECT
// 这类真正的转发规则（配套的 MASQUERADE / ACCEPT 不计入）。
func parseIPTablesRules(out string) []fwRule {
	var rules []fwRule
	counts := map[string]int{} // 每条链上的规则序号
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "-A ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		chain := fields[1]
		counts[chain]++
		pos := counts[chain]

		target := fieldValue(fields, "-j")
		if target != "DNAT" && target != "REDIRECT" {
			continue
		}
		fwd := types.SystemForward{
			Proto:   strings.ToLower(fieldValue(fields, "-p")),
			SrcPort: atoi(fieldValue(fields, "--dport")),
			Backend: fwBackendIPTables,
			Raw:     line,
		}
		if fwd.SrcPort == 0 {
			// 可能是 multiport 或端口范围，退化为仅展示
			if mp := fieldValue(fields, "--dports"); mp != "" {
				fwd.SrcPort = atoi(strings.SplitN(mp, ",", 2)[0])
			}
		}
		comment := strings.Trim(fieldValue(fields, "--comment"), `"`)
		switch target {
		case "DNAT":
			host, port := splitHostPortLoose(fieldValue(fields, "--to-destination"))
			fwd.DestIP = host
			fwd.DestPort = port
		case "REDIRECT":
			fwd.DestPort = atoi(fieldValue(fields, "--to-ports"))
		}
		if fwd.DestIP == "" && fwd.DestPort == 0 {
			fwd.DestPort = fwd.SrcPort
		}
		rule := fwRule{fwd: fwd, table: "nat", chain: chain, pos: pos}
		rule.fwd.Proto = protoOrDefault(rule.fwd.Proto)
		if strings.HasPrefix(comment, fwCommentPrefix) {
			rule.fwd.ID = comment
			rule.fwd.Managed = true
		} else {
			rule.fwd.ID = unmanagedID(fwBackendIPTables, "nat", chain, line)
		}
		rules = append(rules, rule)
	}
	return rules
}

// --- nftables ---

const fwNFTListScript = `exec 2>&1
nft -a list table ip ` + fwNATTable + ` 2>&1
echo "@@END@@"
`

func listNFTablesRules(run fwRun, env *fwEnv) ([]fwRule, error) {
	out, _, err := env.exec(run, fwNFTListScript)
	if err != nil {
		return nil, err
	}
	return parseNFTRules(out), nil
}

// parseNFTRules 解析 `nft -a list table ip spark_nat` 输出，只保留 prerouting
// 链上的 dnat / redirect 规则。
func parseNFTRules(out string) []fwRule {
	var rules []fwRule
	table, chain := "", ""
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "table "):
			f := strings.Fields(line)
			if len(f) >= 3 {
				table = f[1] + " " + f[2]
			}
			continue
		case strings.HasPrefix(line, "chain "):
			f := strings.Fields(line)
			if len(f) >= 2 {
				chain = f[1]
			}
			continue
		case line == "}" || strings.HasPrefix(line, "@@"):
			chain = ""
			continue
		}
		isDNAT := strings.Contains(line, "dnat to")
		isRedirect := strings.Contains(line, "redirect to")
		if !isDNAT && !isRedirect {
			continue
		}
		f := strings.Fields(line)
		fwd := types.SystemForward{
			Proto:   strings.ToLower(fieldBefore(f, "dport")),
			SrcPort: atoi(fieldValue(f, "dport")),
			Backend: fwBackendNFTables,
			Raw:     line,
		}
		if isDNAT {
			host, port := splitHostPortLoose(fieldAfterSeq(f, "dnat", "to"))
			fwd.DestIP, fwd.DestPort = host, port
		} else {
			fwd.DestPort = atoi(fieldAfterSeq(f, "redirect", "to"))
		}
		if fwd.DestIP == "" && fwd.DestPort == 0 {
			fwd.DestPort = fwd.SrcPort
		}
		fwd.Proto = protoOrDefault(fwd.Proto)
		rule := fwRule{fwd: fwd, table: table, chain: chain, handle: nftHandle(line)}
		comment := strings.Trim(nftComment(line), `"`)
		if strings.HasPrefix(comment, fwCommentPrefix) {
			rule.fwd.ID = comment
			rule.fwd.Managed = true
		} else {
			rule.fwd.ID = unmanagedID(fwBackendNFTables, table, chain, line)
		}
		if rule.fwd.SrcPort != 0 {
			rules = append(rules, rule)
		}
	}
	return rules
}

// nftHandle 取出 `... # handle 12` 中的 handle。
func nftHandle(line string) int {
	i := strings.LastIndex(line, "# handle ")
	if i < 0 {
		return 0
	}
	return atoi(strings.TrimSpace(line[i+len("# handle "):]))
}

// nftComment 取出 `comment "spark-fwd-x"` 中的内容。
func nftComment(line string) string {
	f := strings.Fields(line)
	for i, tok := range f {
		if tok == "comment" && i+1 < len(f) {
			return f[i+1]
		}
	}
	return ""
}

// --- firewalld ---

func listFirewalldRules(run fwRun, env *fwEnv, zone string) ([]fwRule, error) {
	// 默认区域 + 本应用在清单里记过的其它区域（否则非默认区域里的规则会“消失”）
	zones := []string{zone}
	seenZone := map[string]bool{zone: true}
	for _, n := range parseSidecar(env.Sidecar).Rules {
		if n.Zone != "" && !seenZone[n.Zone] {
			seenZone[n.Zone] = true
			zones = append(zones, n.Zone)
		}
	}

	var rules []fwRule
	seen := map[string]bool{}
	for _, z := range zones {
		script := `exec 2>&1
Z=` + shQuote(z) + `
echo "@@PERM@@"
firewall-cmd --permanent --zone="$Z" --list-forward-ports 2>&1
echo "@@RUNTIME@@"
firewall-cmd --zone="$Z" --list-forward-ports 2>&1
echo "@@RICH@@"
firewall-cmd --permanent --zone="$Z" --list-rich-rules 2>&1
echo "@@END@@"
`
		out, _, err := env.exec(run, script)
		if err != nil {
			return nil, err
		}
		sec := splitSections(out)
		for _, r := range parseFirewalldForwardPorts(sec["PERM"], z) {
			key := keyWithZone(r.fwd)
			if seen[key] {
				continue
			}
			seen[key] = true
			rules = append(rules, r)
		}
		// 只在 runtime 存在（没写 permanent）的规则也展示出来，提示重启会丢
		for _, r := range parseFirewalldForwardPorts(sec["RUNTIME"], z) {
			key := keyWithZone(r.fwd)
			if seen[key] {
				continue
			}
			seen[key] = true
			r.fwd.Raw = "runtime only: " + r.fwd.Raw
			rules = append(rules, r)
		}
		for _, r := range parseFirewalldRichRules(sec["RICH"], z) {
			key := keyWithZone(r.fwd)
			if seen[key] {
				continue
			}
			seen[key] = true
			rules = append(rules, r)
		}
	}
	return rules, nil
}

func keyWithZone(f types.SystemForward) string {
	return fmt.Sprintf("%s|%d|%d|%s", f.Proto, f.SrcPort, f.DestPort, f.DestIP)
}

// parseFirewalldForwardPorts 解析 `--list-forward-ports` 的空格分隔列表，
// 形如 port=80:proto=tcp:toport=8080:toaddr=10.0.0.5。
func parseFirewalldForwardPorts(out, zone string) []fwRule {
	var rules []fwRule
	for _, item := range strings.Fields(out) {
		if !strings.HasPrefix(item, "port=") {
			continue
		}
		fwd := types.SystemForward{Backend: fwBackendFirewalld, Zone: zone, Raw: item}
		for _, kv := range strings.Split(item, ":") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch k {
			case "port":
				fwd.SrcPort = atoi(v)
			case "proto":
				fwd.Proto = strings.ToLower(v)
			case "toport":
				fwd.DestPort = atoi(v)
			case "toaddr":
				fwd.DestIP = v
			}
		}
		if fwd.SrcPort == 0 {
			continue
		}
		fwd.Proto = protoOrDefault(fwd.Proto)
		if fwd.DestIP == "" && fwd.DestPort == 0 {
			fwd.DestPort = fwd.SrcPort
		}
		rules = append(rules, fwRule{
			fwd:   fwd,
			table: "firewalld",
			chain: zone,
			spec:  firewalldSpec(fwd),
		})
	}
	return rules
}

// parseFirewalldRichRules 解析富规则里的 forward 元素（只做展示与精确删除）。
func parseFirewalldRichRules(out, zone string) []fwRule {
	var rules []fwRule
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.Contains(line, "forward") || !strings.Contains(line, "port") {
			continue
		}
		kv := parseRichAttrs(line)
		fwd := types.SystemForward{
			Backend: fwBackendFirewalld,
			Zone:    zone,
			Proto:   protoOrDefault(strings.ToLower(kv["protocol"])),
			SrcPort: atoi(kv["port"]),
			DestIP:  kv["to-addr"],
			Raw:     line,
		}
		fwd.DestPort = atoi(kv["to-port"])
		if fwd.SrcPort == 0 {
			continue
		}
		if fwd.DestIP == "" && fwd.DestPort == 0 {
			fwd.DestPort = fwd.SrcPort
		}
		rules = append(rules, fwRule{fwd: fwd, table: "firewalld", chain: zone, spec: line})
	}
	return rules
}

// parseRichAttrs 取出富规则里的 key="value" 属性（port / protocol / to-port / to-addr 等）。
func parseRichAttrs(line string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(line); i++ {
		if line[i] != '=' {
			continue
		}
		// 向左取属性名（允许 to-port / to-addr 这类短横线键名）
		j := i - 1
		for j >= 0 && (isWordByte(line[j]) || line[j] == '-') {
			j--
		}
		key := line[j+1 : i]
		if key == "" {
			continue
		}
		// 向右取值（允许引号包裹以及 - . / : 等地址字符）
		k := i + 1
		for k < len(line) && isRichValueByte(line[k]) {
			k++
		}
		val := strings.Trim(line[i+1:k], `"`)
		if val == "" {
			continue
		}
		if _, dup := out[key]; !dup {
			out[key] = val
		}
	}
	return out
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// isRichValueByte 允许富规则属性值里出现的字符（含包裹用的双引号）。
func isRichValueByte(c byte) bool {
	return isWordByte(c) || c == '"' || c == '-' || c == '.' || c == '/' || c == ':'
}

// firewalldSpec 还原 --add/remove-forward-port 的规格串。
func firewalldSpec(f types.SystemForward) string {
	s := fmt.Sprintf("port=%d:proto=%s", f.SrcPort, protoOrDefault(f.Proto))
	if f.DestPort != 0 && f.DestPort != f.SrcPort {
		s += fmt.Sprintf(":toport=%d", f.DestPort)
	}
	if f.DestIP != "" {
		s += ":toaddr=" + f.DestIP
	}
	return s
}

// ---------------------------------------------------------------------------
// 远端清单（备注 / 来源标记）
// ---------------------------------------------------------------------------

// parseSidecar 解析远端清单。文件由本应用写成单行 JSON，这里只取第一条以
// `{` 开头的行，避免被脚本标记等噪音干扰。
func parseSidecar(raw string) *fwSidecar {
	sc := &fwSidecar{Version: 1}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		if err := json.Unmarshal([]byte(line), sc); err != nil {
			return &fwSidecar{Version: 1}
		}
		return sc
	}
	return sc
}

// applySidecar 合并备注，并对 firewalld（富规则不支持注释）判定来源。
// firewalld 按「协议+端口+目标+区域」匹配，规则自身的区域取自解析结果，
// 这样非默认区域里的规则也能对上清单。
func applySidecar(rules []fwRule, env *fwEnv) {
	sc := parseSidecar(env.Sidecar)
	byID := map[string]fwNote{}
	byKey := map[string]fwNote{}
	for _, n := range sc.Rules {
		byID[n.ID] = n
		byKey[sidecarKey(n.Backend, n.Proto, n.SrcPort, n.DestIP, n.DestPort, n.Zone)] = n
	}
	for i := range rules {
		r := &rules[i]
		n, ok := byID[r.fwd.ID]
		if !ok && r.fwd.Backend == fwBackendFirewalld {
			n, ok = byKey[sidecarKey(fwBackendFirewalld, r.fwd.Proto, r.fwd.SrcPort, r.fwd.DestIP, r.fwd.DestPort, r.fwd.Zone)]
			if ok {
				r.fwd.ID = n.ID
			}
		}
		if ok {
			r.fwd.Managed = true
			r.fwd.Note = n.Note
			r.fwd.CreatedAt = n.CreatedAt
		}
	}
}

func sidecarKey(backend, proto string, srcPort int, destIP string, destPort int, zone string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%d|%s", backend, proto, srcPort, destIP, destPort, zone)
}

// sidecarWriteSnippet 生成把清单写回远端的 shell 片段（base64 规避转义问题）。
func sidecarWriteSnippet(sc *fwSidecar) string {
	b, err := json.Marshal(sc)
	if err != nil {
		return ""
	}
	if len(sc.Rules) == 0 {
		return "rm -f " + fwSidecarPath + " 2>/dev/null || true"
	}
	enc := base64.StdEncoding.EncodeToString(b)
	path := shQuote(fwSidecarPath)
	return "mkdir -p \"$(dirname " + path + ")\" 2>/dev/null\n" +
		"printf '%s' " + shQuote(enc) + " | base64 -d > " + path + " 2>/dev/null || true"
}

// ---------------------------------------------------------------------------
// 创建规则
// ---------------------------------------------------------------------------

// validateSystemForward 校验并规范化请求；所有字段随后会直接拼进 shell 脚本，
// 因此必须严格限定取值范围。
func validateSystemForward(req *types.SystemForwardRequest) error {
	req.Proto = strings.ToLower(strings.TrimSpace(req.Proto))
	if req.Proto == "" {
		req.Proto = "tcp"
	}
	if req.Proto != "tcp" && req.Proto != "udp" {
		return errors.New("协议仅支持 tcp / udp")
	}
	if req.SrcPort < 1 || req.SrcPort > 65535 {
		return errors.New("外部端口需在 1-65535 之间")
	}
	if req.DestPort < 1 || req.DestPort > 65535 {
		return errors.New("目标端口需在 1-65535 之间")
	}
	ip := net.ParseIP(strings.TrimSpace(req.DestIP))
	if ip == nil || ip.To4() == nil {
		return errors.New("目标地址需为 IPv4 地址（如 10.0.0.5），暂不支持 IPv6 与域名")
	}
	if ip.IsUnspecified() {
		return errors.New("目标地址不能是 0.0.0.0")
	}
	if ip.IsLoopback() {
		return errors.New("目标地址不能是回环地址（127.0.0.1）：外部流量无法 DNAT 到服务器自身回环，" +
			"转发到本机服务请填写服务器的内网/公网 IP")
	}
	req.DestIP = ip.To4().String()
	req.Note = strings.TrimSpace(req.Note)
	if r := []rune(req.Note); len(r) > 60 {
		req.Note = string(r[:60])
	}
	req.Zone = strings.TrimSpace(req.Zone)
	if req.Zone != "" && !validZoneName(req.Zone) {
		return errors.New("区域名只能包含字母、数字、下划线与短横线")
	}
	return nil
}

func validZoneName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return s != ""
}

// addSystemForwardRule 生成并执行创建脚本，返回可读错误。
func addSystemForwardRule(run fwRun, env *fwEnv, req types.SystemForwardRequest) error {
	sid := fwCommentPrefix + types.NewID()[:12]
	sc := parseSidecar(env.Sidecar)
	sc.Version = 1
	zone := req.Zone
	if zone == "" {
		zone = env.DefaultZone
	}

	var script string
	switch env.Backend {
	case fwBackendFirewalld:
		script = firewalldAddScript(req, sid, zone)
	case fwBackendIPTables:
		script = iptablesAddScript(req, sid, env)
	case fwBackendNFTables:
		script = nftablesAddScript(req, sid, env)
	default:
		return errors.New("未检测到可用的防火墙后端")
	}

	sc.Rules = append(sc.Rules, fwNote{
		ID:        sid,
		Backend:   env.Backend,
		Proto:     req.Proto,
		SrcPort:   req.SrcPort,
		DestIP:    req.DestIP,
		DestPort:  req.DestPort,
		Zone:      zone,
		Note:      req.Note,
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	})
	script += "\n" + sidecarWriteSnippet(sc) + "\necho \"@@OK@@\"\n"

	out, code, err := env.exec(run, script)
	if err != nil {
		return err
	}
	if code != 0 || !strings.Contains(out, "@@OK@@") {
		return fmt.Errorf("创建系统转发失败：%s", fwErrText(out))
	}
	env.Sidecar = mustJSON(sc)
	env.IPForward = true
	return nil
}

// fwErrText 把脚本输出整理成一行可读错误（去掉标记行与空行）。
func fwErrText(out string) string {
	var parts []string
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "@@") {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return "远端未返回任何信息，请检查防火墙工具与权限"
	}
	if len(parts) > 4 {
		parts = parts[len(parts)-4:]
	}
	return strings.Join(parts, "；")
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// iptablesTidySnippet 在自己创建的链已经没有规则时，把跳转和空链一并撤掉，
// 避免用户删光规则后服务器上还留着三条空链。任何一步失败都忽略。
func iptablesTidySnippet() string {
	return `if ! iptables -w -t nat -S ` + fwIPTChainPre + ` 2>/dev/null | grep -q '^-A '; then
  iptables -w -t nat -D PREROUTING -j ` + fwIPTChainPre + ` 2>/dev/null || true
  iptables -w -t nat -X ` + fwIPTChainPre + ` 2>/dev/null || true
fi
if ! iptables -w -t nat -S ` + fwIPTChainPost + ` 2>/dev/null | grep -q '^-A '; then
  iptables -w -t nat -D POSTROUTING -j ` + fwIPTChainPost + ` 2>/dev/null || true
  iptables -w -t nat -X ` + fwIPTChainPost + ` 2>/dev/null || true
fi
if ! iptables -w -t filter -S ` + fwIPTChainFwd + ` 2>/dev/null | grep -q '^-A '; then
  iptables -w -t filter -D FORWARD -j ` + fwIPTChainFwd + ` 2>/dev/null || true
  iptables -w -t filter -X ` + fwIPTChainFwd + ` 2>/dev/null || true
fi`
}

// nftablesTidySnippet 在自己创建的表里已经没有规则时整表删除。
// handle 数量 = 表 + 链 + 规则，减去表与链即为规则数。
func nftablesTidySnippet() string {
	return `for t in "ip ` + fwNATTable + `" "inet ` + fwFilterTable + `"; do
  [ -z "$(nft list table $t 2>/dev/null)" ] && continue
  total=$(nft -a list table $t 2>/dev/null | grep -c '# handle')
  meta=$(nft -a list table $t 2>/dev/null | grep -cE '^[[:space:]]*(table|chain) .*# handle')
  if [ "$total" = "$meta" ]; then nft delete table $t 2>/dev/null || true; fi
done`
}

// ipForwardSnippet 打开并持久化 net.ipv4.ip_forward。
func ipForwardSnippet() string {
	return `if [ "$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null)" != "1" ]; then
  echo 1 > /proc/sys/net/ipv4/ip_forward 2>/dev/null || true
  mkdir -p /etc/sysctl.d 2>/dev/null
  printf 'net.ipv4.ip_forward=1\n' > ` + fwSysctlFile + ` 2>/dev/null || true
fi`
}

// iptablesAddScript 生成 iptables 后端的创建脚本。
func iptablesAddScript(req types.SystemForwardRequest, sid string, env *fwEnv) string {
	p, dp := req.Proto, req.DestPort
	dst := req.DestIP
	lines := []string{
		"exec 2>&1",
		"set -e",
		fmt.Sprintf("iptables -w -t nat -N %s 2>/dev/null || true", fwIPTChainPre),
		fmt.Sprintf("iptables -w -t nat -N %s 2>/dev/null || true", fwIPTChainPost),
		fmt.Sprintf("iptables -w -t filter -N %s 2>/dev/null || true", fwIPTChainFwd),
		fmt.Sprintf("iptables -w -t nat -C PREROUTING -j %s 2>/dev/null || iptables -w -t nat -I PREROUTING 1 -j %s", fwIPTChainPre, fwIPTChainPre),
		fmt.Sprintf("iptables -w -t nat -C POSTROUTING -j %s 2>/dev/null || iptables -w -t nat -A POSTROUTING -j %s", fwIPTChainPost, fwIPTChainPost),
		fmt.Sprintf("iptables -w -t filter -C FORWARD -j %s 2>/dev/null || iptables -w -t filter -I FORWARD 1 -j %s", fwIPTChainFwd, fwIPTChainFwd),
		fmt.Sprintf("iptables -w -t nat -A %s -p %s -m %s --dport %d -m comment --comment %s -j DNAT --to-destination %s:%d",
			fwIPTChainPre, p, p, req.SrcPort, sid, dst, dp),
		fmt.Sprintf("iptables -w -t nat -A %s -d %s/32 -p %s -m %s --dport %d -m comment --comment %s -j MASQUERADE",
			fwIPTChainPost, dst, p, p, dp, sid),
		fmt.Sprintf("iptables -w -t filter -A %s -d %s/32 -p %s -m %s --dport %d -m comment --comment %s -j ACCEPT",
			fwIPTChainFwd, dst, p, p, dp, sid),
		ipForwardSnippet(),
		iptablesPersistSnippet(),
	}
	return strings.Join(lines, "\n") + "\n"
}

// iptablesPersistSnippet 生成 iptables 持久化片段（netfilter-persistent 优先）。
func iptablesPersistSnippet() string {
	dir := fwIPTablesSavePath
	if i := strings.LastIndex(dir, "/"); i > 0 {
		dir = dir[:i]
	}
	return `if command -v netfilter-persistent >/dev/null 2>&1 && netfilter-persistent save >/dev/null 2>&1; then
  command -v systemctl >/dev/null 2>&1 && systemctl enable netfilter-persistent >/dev/null 2>&1
elif [ -f /etc/sysconfig/iptables ] && command -v service >/dev/null 2>&1 && service iptables save >/dev/null 2>&1; then
  :
elif command -v iptables-save >/dev/null 2>&1 && mkdir -p ` + shQuote(dir) + ` 2>/dev/null && iptables-save > ` + shQuote(fwIPTablesSavePath) + ` 2>/dev/null; then
  :
fi`
}

// nftablesAddScript 生成 nftables 后端的创建脚本。
func nftablesAddScript(req types.SystemForwardRequest, sid string, env *fwEnv) string {
	p, dp, dst := req.Proto, req.DestPort, req.DestIP
	lines := []string{
		"exec 2>&1",
		"set -e",
		fmt.Sprintf("nft add table ip %s 2>/dev/null || true", fwNATTable),
		fmt.Sprintf("nft add chain ip %s prerouting '{ type nat hook prerouting priority dstnat - 10 ; policy accept ; }' 2>/dev/null || true", fwNATTable),
		fmt.Sprintf("nft add chain ip %s postrouting '{ type nat hook postrouting priority srcnat + 10 ; policy accept ; }' 2>/dev/null || true", fwNATTable),
		fmt.Sprintf("nft add table inet %s 2>/dev/null || true", fwFilterTable),
		fmt.Sprintf("nft add chain inet %s forward '{ type filter hook forward priority filter - 10 ; policy accept ; }' 2>/dev/null || true", fwFilterTable),
		fmt.Sprintf("nft add rule ip %s prerouting %s dport %d dnat to %s:%d comment %q", fwNATTable, p, req.SrcPort, dst, dp, sid),
		fmt.Sprintf("nft add rule ip %s postrouting ip daddr %s %s dport %d masquerade comment %q", fwNATTable, dst, p, dp, sid),
		fmt.Sprintf("nft add rule inet %s forward ip daddr %s %s dport %d accept comment %q", fwFilterTable, dst, p, dp, sid),
		ipForwardSnippet(),
		nftablesPersistSnippet(),
	}
	return strings.Join(lines, "\n") + "\n"
}

// nftablesPersistSnippet 把 spark 自己的表写进 /etc/nftables.conf 的标记区块，
// 只替换区块内容，不动文件其它部分（首次修改前留一份 .spark.bak 备份）。
func nftablesPersistSnippet() string {
	return `conf=` + shQuote(fwNftConfPath) + `
mkdir -p "$(dirname "$conf")" 2>/dev/null
if [ ! -f "$conf" ]; then printf '#!/usr/sbin/nft -f\n' > "$conf" 2>/dev/null || true; fi
if [ -f "$conf" ] && [ ! -f "$conf.spark.bak" ]; then cp -p "$conf" "$conf.spark.bak" 2>/dev/null || true; fi
if [ -f "$conf" ]; then
  tmp=$(mktemp 2>/dev/null) || tmp=/tmp/spark-nft-$$
  awk '/^# >>> spark port-forward >>>/{s=1} /^# <<< spark port-forward <<<$/{s=0;next} !s' "$conf" > "$tmp" 2>/dev/null || cp "$conf" "$tmp"
  {
    echo "# >>> spark port-forward >>>"
    nft list table ip ` + fwNATTable + ` 2>/dev/null
    nft list table inet ` + fwFilterTable + ` 2>/dev/null
    echo "# <<< spark port-forward <<<"
  } >> "$tmp" 2>/dev/null
  cat "$tmp" > "$conf" 2>/dev/null && rm -f "$tmp" 2>/dev/null
  command -v systemctl >/dev/null 2>&1 && systemctl enable nftables >/dev/null 2>&1
fi`
}

// firewalldAddScript 生成 firewalld 后端的创建脚本。
// 富规则不支持注释，所以来源标记只记录在远端清单里；--add-forward-port 支持
// toaddr，且会在指定 toaddr 时隐式打开 IP 转发。masquerade 是转发能通的前提，
// 因此它失败（且不是「已启用」）时直接报错，避免留下一条通不了的规则。
func firewalldAddScript(req types.SystemForwardRequest, sid, zone string) string {
	spec := firewalldSpec(types.SystemForward{
		Proto: req.Proto, SrcPort: req.SrcPort, DestIP: req.DestIP, DestPort: req.DestPort,
	})
	z := shQuote(zone)
	return `exec 2>&1
Z=` + z + `
OUT=$(firewall-cmd --permanent --zone="$Z" --add-masquerade 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "11" ]; then echo "开启 masquerade 失败：$OUT"; exit 1; fi
OUT=$(firewall-cmd --zone="$Z" --add-masquerade 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "11" ]; then echo "开启 masquerade 失败：$OUT"; exit 1; fi
OUT=$(firewall-cmd --permanent --zone="$Z" --add-forward-port=` + spec + ` 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "11" ]; then echo "$OUT"; exit 1; fi
OUT=$(firewall-cmd --zone="$Z" --add-forward-port=` + spec + ` 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "11" ]; then echo "$OUT"; exit 1; fi
echo "firewalld: $Z ` + spec + `"
`
}

// ---------------------------------------------------------------------------
// 删除规则
// ---------------------------------------------------------------------------

// removeSystemForwardRule 按规则 ID 精确删除。managed 规则会连带清掉配套的
// MASQUERADE / ACCEPT 规则与远端清单项。
func removeSystemForwardRule(run fwRun, env *fwEnv, ruleID string) error {
	rules, err := listSystemForwardRules(run, env, env.DefaultZone)
	if err != nil {
		return err
	}
	var target *fwRule
	for i := range rules {
		if rules[i].fwd.ID == ruleID {
			target = &rules[i]
			break
		}
	}
	if target == nil {
		// 规则可能已经被别处删掉，刷新清单即可
		pruneSidecar(env, ruleID)
		return nil
	}

	var script string
	switch env.Backend {
	case fwBackendFirewalld:
		// 富规则 / forward-port 单条删除，区域级 masquerade 保留（可能为其它规则服务）
		script = firewalldRemoveScript(*target)
	case fwBackendIPTables:
		if target.fwd.Managed {
			script = iptablesDeleteByComment(target.fwd.ID) + "\n" + iptablesTidySnippet()
		} else {
			script = iptablesRemoveScript(*target)
		}
		script += "\n" + iptablesPersistSnippet()
	case fwBackendNFTables:
		if target.fwd.Managed {
			script = nftablesDeleteByComment(target.fwd.ID) + "\n" + nftablesTidySnippet()
		} else {
			script = nftablesRemoveScript(*target)
		}
		script += "\n" + nftablesPersistSnippet()
	default:
		return errors.New("未检测到可用的防火墙后端")
	}

	pruneSidecar(env, ruleID)
	script += "\n" + sidecarWriteSnippet(parseSidecar(env.Sidecar))
	script += "\necho \"@@OK@@\"\n"

	out, code, err := env.exec(run, script)
	if err != nil {
		return err
	}
	if code != 0 || !strings.Contains(out, "@@OK@@") {
		return fmt.Errorf("删除系统转发失败：%s", fwErrText(out))
	}
	return nil
}

// pruneSidecar 从内存里的清单移除一条记录。
func pruneSidecar(env *fwEnv, ruleID string) {
	sc := parseSidecar(env.Sidecar)
	kept := sc.Rules[:0]
	for _, n := range sc.Rules {
		if n.ID != ruleID {
			kept = append(kept, n)
		}
	}
	sc.Rules = kept
	sc.Version = 1
	env.Sidecar = mustJSON(sc)
}

func iptablesRemoveScript(r fwRule) string {
	if r.fwd.Managed {
		return iptablesDeleteByComment(r.fwd.ID)
	}
	return fmt.Sprintf("exec 2>&1\nset -e\niptables -w -t %s -D %s %d\n", r.table, r.chain, r.pos)
}

// iptablesDeleteByComment 删除某条链上所有带指定注释的规则（按序号从后往前）。
func iptablesDeleteByComment(cid string) string {
	return `spark_del() {
  tbl=$1; ch=$2
  iptables -w -t "$tbl" -S "$ch" 2>/dev/null | awk -v id=` + shQuote(cid) + ` '/^-A /{n++; if (index($0,id)>0) print n}' | sort -rn | while read -r n; do
    iptables -w -t "$tbl" -D "$ch" "$n" 2>/dev/null || true
  done
}
spark_del nat ` + fwIPTChainPre + `
spark_del nat ` + fwIPTChainPost + `
spark_del filter ` + fwIPTChainFwd
}

func nftablesRemoveScript(r fwRule) string {
	if r.fwd.Managed {
		return nftablesDeleteByComment(r.fwd.ID)
	}
	if r.handle == 0 {
		return "echo '缺少 nftables handle，无法删除' >&2; exit 1"
	}
	return fmt.Sprintf("exec 2>&1\nnft delete rule %s %s handle %d\n", r.table, r.chain, r.handle)
}

// nftablesDeleteByComment 按注释删除 spark 自己的规则（先取 handle 再删）。
func nftablesDeleteByComment(cid string) string {
	return `for tbl in "ip ` + fwNATTable + `" "inet ` + fwFilterTable + `"; do
  nft -a list table $tbl 2>/dev/null | while IFS= read -r line; do
    case "$line" in *chain\ *) ch=$(printf '%s' "$line" | awk '{print $2}');; esac
    case "$line" in
      *` + cid + `*)
        h=$(printf '%s' "$line" | sed -n 's/.*# handle \([0-9]*\).*/\1/p')
        [ -n "$h" ] && nft delete rule $tbl "$ch" handle "$h" 2>/dev/null
        ;;
    esac
  done
done`
}

func firewalldRemoveScript(r fwRule) string {
	z := shQuote(r.chain)
	if strings.HasPrefix(r.spec, "rule ") {
		return `exec 2>&1
Z=` + z + `
firewall-cmd --permanent --zone="$Z" --remove-rich-rule=` + shQuote(r.spec) + ` >/dev/null 2>&1 || true
firewall-cmd --zone="$Z" --remove-rich-rule=` + shQuote(r.spec) + ` >/dev/null 2>&1 || true
`
	}
	spec := r.spec
	if spec == "" {
		spec = firewalldSpec(r.fwd)
	}
	return `exec 2>&1
Z=` + z + `
RC=0
OUT=$(firewall-cmd --permanent --zone="$Z" --remove-forward-port=` + spec + ` 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "12" ]; then echo "$OUT"; exit 1; fi
OUT=$(firewall-cmd --zone="$Z" --remove-forward-port=` + spec + ` 2>&1); RC=$?
if [ "$RC" != "0" ] && [ "$RC" != "12" ]; then echo "$OUT"; exit 1; fi
`
}

// ---------------------------------------------------------------------------
// TerminalService 对外方法
// ---------------------------------------------------------------------------

// fwRunner 绑定会话并返回命令执行器。
func (t *TerminalService) fwRunner(sessionID string) (fwRun, error) {
	if t.get(sessionID) == nil {
		return nil, fmt.Errorf("会话 %q 不存在或已断开", sessionID)
	}
	return func(command string) (string, int, error) {
		out, code, err := t.runCommandWithTimeout(sessionID, command, 45*time.Second)
		if err != nil {
			return out, code, err
		}
		return stripTrailingMarker(out), code, nil
	}, nil
}

// stripTrailingMarker 去掉脚本最后的 @@END@@ 标记行。
func stripTrailingMarker(out string) string {
	return strings.ReplaceAll(out, "@@END@@", "")
}

// SystemForwardStatus 探测远端防火墙后端、权限与已有的转发规则。
func (t *TerminalService) SystemForwardStatus(id string) (*types.SystemForwardStatus, error) {
	run, err := t.fwRunner(id)
	if err != nil {
		return nil, err
	}
	env, err := probeFirewallEnv(run)
	if err != nil {
		return nil, err
	}
	return buildSystemForwardStatus(run, env), nil
}

// AddSystemForward 创建一条系统级（防火墙 NAT）端口转发规则并持久化。
func (t *TerminalService) AddSystemForward(id string, req types.SystemForwardRequest) (*types.SystemForwardStatus, error) {
	run, err := t.fwRunner(id)
	if err != nil {
		return nil, err
	}
	if err := validateSystemForward(&req); err != nil {
		return nil, err
	}
	env, err := probeFirewallEnv(run)
	if err != nil {
		return nil, err
	}
	switch {
	case env.Backend == fwBackendNone:
		return nil, errors.New("远端未检测到 firewalld / iptables / nftables，无法创建系统转发")
	case !env.privileged():
		return nil, errors.New("需要 root 权限（或免密 sudo）才能创建系统转发规则")
	}
	if req.Zone == "" {
		req.Zone = env.DefaultZone
	}
	if err := addSystemForwardRule(run, env, req); err != nil {
		return nil, err
	}
	return buildSystemForwardStatus(run, env), nil
}

// RemoveSystemForward 删除一条系统转发规则（按规则 ID）。
func (t *TerminalService) RemoveSystemForward(id, ruleID string) (*types.SystemForwardStatus, error) {
	run, err := t.fwRunner(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ruleID) == "" {
		return nil, errors.New("缺少规则 ID")
	}
	env, err := probeFirewallEnv(run)
	if err != nil {
		return nil, err
	}
	switch {
	case env.Backend == fwBackendNone:
		return nil, errors.New("远端未检测到 firewalld / iptables / nftables，无法删除系统转发")
	case !env.privileged():
		return nil, errors.New("需要 root 权限（或免密 sudo）才能修改系统转发规则")
	}
	if err := removeSystemForwardRule(run, env, ruleID); err != nil {
		return nil, err
	}
	return buildSystemForwardStatus(run, env), nil
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func sectionFirst(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func sectionHas(s, want string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// hasAnyLine 判断某一行是否包含子串（用于 `command -v xxx` 输出）。
func hasAnyLine(s, sub string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, sub) {
			return true
		}
	}
	return false
}

// fieldValue 返回字段列表中 key 之后的第一个字段（iptables 规则解析用）。
func fieldValue(fields []string, key string) string {
	for i, f := range fields {
		if f == key && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// fieldBefore 返回 key 之前的第一个字段（如 `tcp dport 80` → tcp）。
func fieldBefore(fields []string, key string) string {
	for i, f := range fields {
		if f == key && i > 0 {
			return fields[i-1]
		}
	}
	return ""
}

// fieldAfterSeq 返回 a、b 两个连续字段之后的字段（如 `dnat to 10.0.0.1:80`）。
func fieldAfterSeq(fields []string, a, b string) string {
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] == a && fields[i+1] == b {
			return fields[i+2]
		}
	}
	return ""
}

// splitHostPortLoose 解析 `host:port` / `host` / `:port`。
func splitHostPortLoose(s string) (string, int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0
	}
	if strings.HasPrefix(s, ":") {
		return "", atoi(s[1:])
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, atoi(p)
	}
	return s, 0
}

func protoOrDefault(p string) string {
	if p == "udp" {
		return "udp"
	}
	return "tcp"
}

// unmanagedID 为系统里已有的规则生成稳定 ID。
func unmanagedID(backend, table, chain, raw string) string {
	sum := sha1.Sum([]byte(backend + "|" + table + "|" + chain + "|" + raw))
	return "sys-" + hex.EncodeToString(sum[:])[:12]
}
