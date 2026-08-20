package terminal

import (
	"strings"
	"testing"
)

func TestParseServerInfo(t *testing.T) {
	out := `@@HOSTNAME@@
web-01
@@OSRELEASE@@
PRETTY_NAME="Ubuntu 24.04 LTS"
VERSION_ID="24.04"
@@KERNEL@@
6.8.0-45-generic
@@ARCH@@
x86_64
@@UPTIME@@
90061.23 123456.78
@@CPUINFO@@
processor	: 0
vendor_id	: GenuineIntel
model name	: Intel(R) Xeon(R) Platinum 8255C CPU @ 2.50GHz
processor	: 1
model name	: Intel(R) Xeon(R) Platinum 8255C CPU @ 2.50GHz
@@NPROC@@
8
@@LOAD@@
0.12 0.34 0.56 1/234 5678
@@MEMINFO@@
MemTotal:       16384000 kB
MemFree:         5120000 kB
MemAvailable:   10240000 kB
Buffers:          234567 kB
@@DISKS@@
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/vda1       51474048  25472704  25850308      50% /
tmpfs           4096000         0   4096000       0% /dev/shm
/dev/vdb1      103080960  12000000  85800960      13% /data
`
	info := parseServerInfo(out)

	if info.Hostname != "web-01" {
		t.Errorf("hostname: %q", info.Hostname)
	}
	if info.OS != "Ubuntu 24.04 LTS" {
		t.Errorf("os: %q", info.OS)
	}
	if info.Kernel != "6.8.0-45-generic" || info.Arch != "x86_64" {
		t.Errorf("kernel/arch: %q %q", info.Kernel, info.Arch)
	}
	if info.Uptime == "" {
		t.Error("uptime empty")
	}
	if !strings.Contains(info.CPUModel, "Xeon") {
		t.Errorf("cpu model: %q", info.CPUModel)
	}
	if info.CPUCores != 2 { // 以 /proc/cpuinfo 的 processor 计数为准
		t.Errorf("cores: %d", info.CPUCores)
	}
	if info.Load1 != 0.12 || info.Load5 != 0.34 || info.Load15 != 0.56 {
		t.Errorf("load: %v %v %v", info.Load1, info.Load5, info.Load15)
	}
	if info.MemoryTotal != 16384000*1024 || info.MemoryAvail != 10240000*1024 {
		t.Errorf("mem: %d %d", info.MemoryTotal, info.MemoryAvail)
	}
	if info.MemoryUsed != (16384000-10240000)*1024 {
		t.Errorf("mem used: %d", info.MemoryUsed)
	}
	// 磁盘：过滤 tmpfs，保留 /dev 两块
	if len(info.Disks) != 2 {
		t.Fatalf("disks: %d, want 2 (%+v)", len(info.Disks), info.Disks)
	}
	d0 := info.Disks[0]
	if d0.Mount != "/" || d0.UsePercent != 50 || d0.Size != 51474048*1024 {
		t.Errorf("disk0: %+v", d0)
	}
	d1 := info.Disks[1]
	if d1.Mount != "/data" || d1.UsePercent != 13 {
		t.Errorf("disk1: %+v", d1)
	}
	if info.Error != "" {
		t.Errorf("unexpected error note: %q", info.Error)
	}
}

func TestParseServerInfoARM(t *testing.T) {
	out := `@@HOSTNAME@@
rpi
@@OSRELEASE@@
PRETTY_NAME="Raspbian GNU/Linux 11 (bullseye)"
@@KERNEL@@
5.15.61-v8+
@@ARCH@@
aarch64
@@UPTIME@@
100
@@CPUINFO@@
processor	: 0
Hardware	: BCM2835
processor	: 1
processor	: 2
processor	: 3
@@NPROC@@
4
@@LOAD@@
0.01 0.02 0.03 1/100 200
@@MEMINFO@@
MemTotal:         941616 kB
MemFree:          500000 kB
MemAvailable:     600000 kB
@@DISKS@@
Filesystem     1024-blocks      Used Available Capacity Mounted on
/dev/root         30303988   5529616  23193468      20% /
`
	info := parseServerInfo(out)
	if info.CPUCores != 4 {
		t.Errorf("arm cores: %d", info.CPUCores)
	}
	if info.CPUModel != "BCM2835" {
		t.Errorf("arm model: %q", info.CPUModel)
	}
	if info.Arch != "aarch64" {
		t.Errorf("arch: %q", info.Arch)
	}
	if len(info.Disks) != 1 || info.Disks[0].Mount != "/" {
		t.Errorf("disks: %+v", info.Disks)
	}
	if info.Error != "" {
		t.Errorf("unexpected error: %q", info.Error)
	}
}

func TestParseServerInfoNonLinux(t *testing.T) {
	out := `@@HOSTNAME@@
@@OSRELEASE@@
@@KERNEL@@
@@ARCH@@
@@UPTIME@@
@@CPUINFO@@
@@NPROC@@
@@LOAD@@
@@MEMINFO@@
@@DISKS@@
`
	info := parseServerInfo(out)
	if info.Error == "" {
		t.Error("expected non-linux error note")
	}
}

func TestParseServerInfoPartial(t *testing.T) {
	// Windows 之类：hostname 有，/proc 无 → 不报"非 Linux"，静默展示已有信息
	out := `@@HOSTNAME@@
winhost
@@OSRELEASE@@
@@KERNEL@@
@@ARCH@@
@@UPTIME@@
@@CPUINFO@@
@@NPROC@@
@@LOAD@@
@@MEMINFO@@
@@DISKS@@
`
	info := parseServerInfo(out)
	if info.Hostname != "winhost" {
		t.Errorf("hostname: %q", info.Hostname)
	}
	if info.Error != "" {
		t.Errorf("partial info should not report error, got %q", info.Error)
	}
}

func TestShQuote(t *testing.T) {
	got := shQuote("a'b\nc")
	want := `'a'\''b
c'`
	if got != want {
		t.Errorf("shQuote: %q != %q", got, want)
	}
}

func TestParsePsEO(t *testing.T) {
	out := `PID PPID USER %CPU %MEM RSS STAT COMMAND
  123     1 root  0.5  1.2  45678 Ss   /usr/sbin/sshd -D
  456   123 root 12.3  0.8  12345 Sl   nginx: worker process
  789     1 user  0.0  0.1   1024 R    top
`
	list := parsePsEO(out)
	if len(list) != 3 {
		t.Fatalf("want 3 processes, got %d", len(list))
	}
	p := list[1]
	if p.PID != 456 || p.PPID != 123 || p.User != "root" || p.CPU != 12.3 || p.Mem != 0.8 || p.RSS != 12345 {
		t.Errorf("process parse: %+v", p)
	}
	if p.Stat != "Sl" || p.Command != "nginx: worker process" {
		t.Errorf("stat/command: %q %q", p.Stat, p.Command)
	}
}

func TestParsePsAux(t *testing.T) {
	out := `USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND
root   1  0.0  0.1 16896 11388 ?   Ss   10:00  0:01 /sbin/init
root 123 95.0  2.0 99999 88888 ?   R    11:00  5:00 some busy process
`
	list := parsePsAux(out)
	if len(list) != 2 {
		t.Fatalf("want 2 processes, got %d", len(list))
	}
	p := list[1]
	if p.PID != 123 || p.CPU != 95.0 || p.RSS != 88888 || p.Stat != "R" {
		t.Errorf("process parse: %+v", p)
	}
	if p.Command != "some busy process" {
		t.Errorf("command: %q", p.Command)
	}
	if p.PPID != 0 {
		t.Errorf("ppid should be 0 in aux fallback, got %d", p.PPID)
	}
}
