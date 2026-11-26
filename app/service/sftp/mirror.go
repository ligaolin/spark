package sftp

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"spark/app/service/types"

	sftplib "github.com/pkg/sftp"
)

// mirrorMaxFiles 限制一次比较的文件数量，防止误选大目录导致遍历过久。
const mirrorMaxFiles = 200000

type mirrorFile struct {
	size  int64
	mtime time.Time
}

// CompareMirror 比较本地目录与远程目录树，返回差异清单（dry-run，不做任何修改）。
// direction 为 "upload"（本地 → 远程）或 "download"（远程 → 本地）；
// deleteExtra 为 true 时，把目标端多余的文件也列为删除项（仅文件，不动目录）。
func (s *SFTPFileService) CompareMirror(id, localDir, remoteDir, direction string, deleteExtra bool) (types.MirrorDiff, error) {
	sess, err := s.get(id)
	if err != nil {
		return types.MirrorDiff{}, err
	}
	if direction != "upload" && direction != "download" {
		return types.MirrorDiff{}, errors.New("同步方向无效")
	}
	if strings.TrimSpace(localDir) == "" || strings.TrimSpace(remoteDir) == "" {
		return types.MirrorDiff{}, errors.New("本地目录和远程目录都不能为空")
	}
	if fi, err := os.Stat(localDir); err != nil {
		return types.MirrorDiff{}, errors.New("本地目录不可用：" + err.Error())
	} else if !fi.IsDir() {
		return types.MirrorDiff{}, errors.New("本地路径不是目录")
	}
	if st, serr := sess.sftp.Stat(remoteDir); serr == nil && !st.IsDir() {
		return types.MirrorDiff{}, errors.New("远程路径不是目录")
	}

	locals := make(map[string]mirrorFile)
	if err := walkLocalFiles(localDir, "", locals); err != nil {
		return types.MirrorDiff{}, err
	}
	remotes := make(map[string]mirrorFile)
	if err := walkRemoteFiles(sess.sftp, remoteDir, "", remotes); err != nil {
		return types.MirrorDiff{}, err
	}
	if len(locals)+len(remotes) > mirrorMaxFiles {
		return types.MirrorDiff{}, errors.New("目录文件数量过多（超过 20 万），请缩小同步范围")
	}

	var items []types.MirrorItem
	add := func(rel, action, reason string, size int64) {
		items = append(items, types.MirrorItem{Rel: rel, Action: action, Reason: reason, Size: size})
	}
	if direction == "upload" {
		for rel, l := range locals {
			r, ok := remotes[rel]
			switch {
			case !ok:
				add(rel, "upload", "new", l.size)
			case l.size != r.size || l.mtime.Sub(r.mtime) > 2*time.Second:
				add(rel, "upload", "changed", l.size)
			}
		}
		if deleteExtra {
			for rel := range remotes {
				if _, ok := locals[rel]; !ok {
					add(rel, "delete-remote", "extra", 0)
				}
			}
		}
	} else {
		for rel, r := range remotes {
			l, ok := locals[rel]
			switch {
			case !ok:
				add(rel, "download", "new", r.size)
			case r.size != l.size || r.mtime.Sub(l.mtime) > 2*time.Second:
				add(rel, "download", "changed", r.size)
			}
		}
		if deleteExtra {
			for rel := range locals {
				if _, ok := remotes[rel]; !ok {
					add(rel, "delete-local", "extra", 0)
				}
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Rel < items[j].Rel })
	diff := types.MirrorDiff{Items: items}
	for _, it := range items {
		if it.Action == "upload" || it.Action == "download" {
			diff.Bytes += it.Size
		}
	}
	return diff, nil
}

func walkLocalFiles(root, prefix string, out map[string]mirrorFile) error {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(prefix)))
	if err != nil {
		if prefix == "" {
			return err
		}
		return nil // 子目录不可读时跳过，不中断整体比较
	}
	for _, e := range entries {
		rel := path.Join(prefix, e.Name())
		if e.IsDir() {
			if err := walkLocalFiles(root, rel, out); err != nil {
				return err
			}
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		out[rel] = mirrorFile{size: info.Size(), mtime: info.ModTime()}
	}
	return nil
}

func walkRemoteFiles(cl *sftplib.Client, root, prefix string, out map[string]mirrorFile) error {
	entries, err := cl.ReadDir(path.Join(root, prefix))
	if err != nil {
		if prefix == "" {
			return err
		}
		return nil // 子目录不可读时跳过，不中断整体比较
	}
	for _, e := range entries {
		rel := path.Join(prefix, e.Name())
		if e.IsDir() {
			if err := walkRemoteFiles(cl, root, rel, out); err != nil {
				return err
			}
			continue
		}
		out[rel] = mirrorFile{size: e.Size(), mtime: e.ModTime()}
	}
	return nil
}