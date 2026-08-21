package ftp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	ftplib "github.com/jlaffaye/ftp"
)

// 模拟"非标准"FTP 服务器：对 RETR 回复 200 "Ready to proceed"（而不是标准 150）。
// 上游 jlaffaye/ftp 会把 200 视为错误（报 200 "Ready to proceed"），本地补丁后应能正常读取。
// 可选 failFirstData：第一次被动数据监听立即关闭（模拟端口抖动/拒绝连接），
// 验证补丁的数据连接重试逻辑。
type quirkyFtpServer struct {
	ln             net.Listener
	data           net.Listener
	failFirstData  bool
	dataAttempts   int
}

func startQuirkyFtpServer(t *testing.T, failFirstData bool) *quirkyFtpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &quirkyFtpServer{ln: ln, failFirstData: failFirstData}
	go s.serve(t)
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *quirkyFtpServer) addr() string { return s.ln.Addr().String() }

func (s *quirkyFtpServer) serve(t *testing.T) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(t, conn)
	}
}

func (s *quirkyFtpServer) handle(t *testing.T, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	write := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	write("220 welcome")

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := line
		if i := strings.IndexByte(line, ' '); i >= 0 {
			cmd = line[:i]
		}
		switch strings.ToUpper(cmd) {
		case "USER":
			write("331 need password")
		case "PASS":
			write("230 logged in")
		case "TYPE":
			write("200 OK")
		case "EPSV":
			// 打开被动数据监听，返回 229
			dl, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 cannot open data conn")
				continue
			}
			s.data = dl
			defer dl.Close()
			s.dataAttempts++
			port := dl.Addr().(*net.TCPAddr).Port
			// 模拟端口抖动：第一次把监听器立即关闭但仍广播该端口，
			// 客户端连接被拒绝后应重试（重新 EPSV）
			if s.failFirstData && s.dataAttempts == 1 {
				_ = dl.Close()
				s.data = nil
			}
			write(fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", port))
		case "PASV":
			dl, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 cannot open data conn")
				continue
			}
			s.data = dl
			defer dl.Close()
			p := dl.Addr().(*net.TCPAddr).Port
			h, _, _ := net.SplitHostPort(dl.Addr().String())
			write(fmt.Sprintf("227 Entering Passive Mode (%s,%d,%d)", strings.ReplaceAll(h, ".", ","), p/256, p%256))
		case "RETR":
			// 非标准：用 200 表示"数据连接就绪，继续"（标准应为 150）
			write("200 Ready to proceed")
			dl := s.data
			if dl == nil {
				write("425 no data conn")
				continue
			}
			dconn, err := dl.Accept()
			if err != nil {
				write("426 data conn failed")
				continue
			}
			_, _ = io.WriteString(dconn, "hello file content\n")
			_ = dconn.Close()
			write("226 Transfer complete")
		case "SIZE":
			write("213 19")
		case "QUIT":
			write("221 bye")
			return
		default:
			write("200 OK")
		}
	}
}

func TestFtpRetr200ReadyToProceed(t *testing.T) {
	srv := startQuirkyFtpServer(t, false)
	conn, err := ftplib.Dial(srv.addr(), ftplib.DialWithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Quit()
	if err := conn.Login("u", "p"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := conn.Type(ftplib.TransferTypeBinary); err != nil {
		t.Fatalf("type: %v", err)
	}

	// 服务器对 RETR 回复 200 "Ready to proceed" —— 补丁后应能正常读文件
	rc, err := conn.Retr("/test.txt")
	if err != nil {
		t.Fatalf("retr with 200 response should succeed, got: %v", err)
	}
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if string(data) != "hello file content\n" {
		t.Fatalf("content wrong: %q", string(data))
	}
}

// 第一次被动数据连接被拒绝时，补丁应自动重试成功
func TestFtpDataConnRetry(t *testing.T) {
	srv := startQuirkyFtpServer(t, true)
	conn, err := ftplib.Dial(srv.addr(), ftplib.DialWithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Quit()
	if err := conn.Login("u", "p"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := conn.Type(ftplib.TransferTypeBinary); err != nil {
		t.Fatalf("type: %v", err)
	}

	rc, err := conn.Retr("/test.txt")
	if err != nil {
		t.Fatalf("retr should retry and succeed, got: %v", err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "hello file content\n" {
		t.Fatalf("content wrong: %q", string(data))
	}
	if srv.dataAttempts < 2 {
		t.Fatalf("expected at least 2 data attempts, got %d", srv.dataAttempts)
	}
}

// 确保端口号拼接正确（供调试）
func TestQuirkyServerPort(t *testing.T) {
	_, portStr, _ := net.SplitHostPort(startQuirkyFtpServer(t, false).addr())
	port, _ := strconv.Atoi(portStr)
	if port <= 0 {
		t.Fatalf("bad port %d", port)
	}
}
