package sftp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/sshconn"
	"spark/app/service/sshlib"

	sftplib "github.com/pkg/sftp"
)

// RelayTo 把 srcID 会话中的远程文件 / 目录流式复制到已保存连接 targetID 的
// destDir（目标服务器）。数据只在应用内存中中转，不落本地磁盘。
// 返回目标端的最终路径（destDir/源文件名）。
func (s *SFTPFileService) RelayTo(srcID, srcPath string, targetID uint, destDir string) (string, error) {
	src, err := s.get(srcID)
	if err != nil {
		return "", err
	}
	if targetID == 0 {
		return "", errors.New("请选择目标服务器")
	}
	destDir = strings.TrimSpace(destDir)
	if destDir == "" || !strings.HasPrefix(destDir, "/") {
		return "", errors.New("目标目录必须是绝对路径")
	}
	fi, err := src.sftp.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("读取源路径失败: %w", err)
	}

	var cn model.SavedConnection
	if err := db.GetDB().First(&cn, targetID).Error; err != nil {
		return "", errors.New("目标服务器不存在")
	}
	if cn.Type == "ftp" {
		return "", errors.New("目标服务器是 FTP，不支持 SFTP 直传")
	}

	client, err := sshlib.Dial(sshconn.Resolve(sshconn.OptionsFromSaved(cn)))
	if err != nil {
		if hk := sshlib.AsHostKeyError(err); hk != nil {
			return "", hk
		}
		return "", fmt.Errorf("连接目标服务器失败: %w", err)
	}
	defer client.Close()
	dst, err := sftplib.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("建立目标 SFTP 会话失败: %w", err)
	}
	defer dst.Close()

	if err := ensureRemoteDir(dst, destDir); err != nil {
		return "", err
	}
	target := path.Join(destDir, path.Base(srcPath))
	sameServer := cn.Host == src.host && cn.Port == src.port && cn.Username == src.user
	// 同一台服务器上把文件复制到自己身上会先截断源文件；把目录复制进自身
	// 会导致无限递归，两种情况都直接拒绝
	if sameServer {
		if path.Clean(target) == path.Clean(srcPath) {
			return "", errors.New("源与目标是同一个文件，无法直传")
		}
		if fi.IsDir() && strings.HasPrefix(path.Clean(destDir)+"/", strings.TrimSuffix(path.Clean(srcPath), "/")+"/") {
			return "", errors.New("目标目录不能位于源目录内部")
		}
	}
	if !fi.IsDir() {
		counter := &transferCounter{id: srcID, op: "copy", name: fi.Name(), total: fi.Size()}
		if err := relayFile(src.sftp, dst, srcPath, target, counter); err != nil {
			return "", err
		}
		return target, nil
	}
	if err := relayDir(src.sftp, dst, srcPath, target, srcID); err != nil {
		return "", err
	}
	return target, nil
}

// relayFile streams one file from the source SFTP client to the target one.
func relayFile(src, dst *sftplib.Client, srcPath, dstPath string, counter *transferCounter) error {
	r, err := src.Open(srcPath)
	if err != nil {
		return fmt.Errorf("打开源文件 %s 失败: %w", srcPath, err)
	}
	defer r.Close()
	if err := ensureRemoteDir(dst, path.Dir(dstPath)); err != nil {
		return err
	}
	w, err := dst.Create(dstPath)
	if err != nil {
		return fmt.Errorf("创建目标文件 %s 失败: %w", dstPath, err)
	}
	defer w.Close()
	if _, err := io.Copy(w, &progressReader{r: r, c: counter}); err != nil {
		return fmt.Errorf("传输 %s 失败: %w", srcPath, err)
	}
	return nil
}

// relayDir recursively copies a directory tree between two SFTP clients.
func relayDir(src, dst *sftplib.Client, srcDir, dstDir, sessionID string) error {
	entries, err := src.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("读取源目录 %s 失败: %w", srcDir, err)
	}
	if err := ensureRemoteDir(dst, dstDir); err != nil {
		return err
	}
	for _, e := range entries {
		if e.Mode()&os.ModeSymlink != 0 {
			continue // 符号链接不跟随，避免循环引用
		}
		sp := path.Join(srcDir, e.Name())
		dp := path.Join(dstDir, e.Name())
		if e.IsDir() {
			if err := relayDir(src, dst, sp, dp, sessionID); err != nil {
				return err
			}
			continue
		}
		counter := &transferCounter{id: sessionID, op: "copy", name: path.Base(dstDir), total: e.Size()}
		if err := relayFile(src, dst, sp, dp, counter); err != nil {
			return err
		}
	}
	return nil
}