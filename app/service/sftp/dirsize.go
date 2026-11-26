package sftp

import (
	"strconv"
	"strings"

	"spark/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

// DirSize returns the total apparent size of a remote directory tree. A
// server-side `du -sb` costs one round trip; when du is unavailable the tree
// is walked with concurrent ReadDir calls instead.
func (s *SFTPFileService) DirSize(id, remotePath string) (types.DirSizeInfo, error) {
	sess, err := s.get(id)
	if err != nil {
		return types.DirSizeInfo{}, err
	}
	if strings.TrimSpace(remotePath) == "" {
		if remotePath, err = sess.sftp.Getwd(); err != nil {
			return types.DirSizeInfo{}, err
		}
	}

	if size, ok := duSize(sess.client, remotePath); ok {
		return types.DirSizeInfo{Size: size, Files: -1}, nil
	}

	st := &searchState{}
	rep := newSearchProgress("", id) // 仅复用遍历逻辑，不推送进度事件
	frontier := []string{remotePath}
	var size, files int64
	for len(frontier) > 0 {
		next, entries := s.listLevel(sess, frontier, nil, st, rep)
		for _, e := range entries {
			if e.isDir {
				continue
			}
			files++
			if e.size > 0 {
				size += e.size
			}
		}
		frontier = next
	}
	return types.DirSizeInfo{Size: size, Files: files}, nil
}

// duSize runs `du -sb` on the server; ok=false means du is missing or failed.
func duSize(client *xssh.Client, dir string) (int64, bool) {
	session, err := client.NewSession()
	if err != nil {
		return 0, false
	}
	defer session.Close()
	// 部分子目录不可读时 du 会以非 0 退出但仍输出已统计的总量，因此只看输出是否可解析。
	out, _ := session.Output("du -sb -- " + shQuote(dir) + " 2>/dev/null")
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0, false
	}
	size, perr := strconv.ParseInt(fields[0], 10, 64)
	if perr != nil || size < 0 {
		return 0, false
	}
	return size, true
}