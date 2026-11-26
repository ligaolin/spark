// Package batchexec runs one shell command on several saved SSH connections
// concurrently and returns each server's output.
package batchexec

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/sshconn"
	"spark/app/service/sshlib"
	"spark/app/service/types"

	xssh "golang.org/x/crypto/ssh"
)

const (
	workerLimit   = 8
	maxOutputSize = 256 << 10 // 单个服务器最多返回 256KB 输出
)

// BatchService exposes multi-server command execution to the frontend.
type BatchService struct{}

// ServiceName implements application.ServiceName.
func (b *BatchService) ServiceName() string { return "BatchService" }

// Execute runs command on every connection id (SSH only) concurrently.
// Per-server failures are reported inside the results; the error return is
// only for invalid input.
func (b *BatchService) Execute(connIDs []uint, command string, timeoutSec int) ([]types.BatchExecResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("命令不能为空")
	}
	if len(connIDs) == 0 {
		return nil, errors.New("请先选择服务器")
	}
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 600 {
		timeoutSec = 600
	}

	results := make([]types.BatchExecResult, len(connIDs))
	sem := make(chan struct{}, workerLimit)
	var wg sync.WaitGroup
	for i, id := range connIDs {
		var cn model.SavedConnection
		if err := db.GetDB().First(&cn, id).Error; err != nil {
			results[i] = types.BatchExecResult{ConnID: id, Error: "连接不存在"}
			continue
		}
		if cn.Type == "ftp" {
			results[i] = types.BatchExecResult{ConnID: id, Name: cn.Name, Host: cn.Host, Error: "FTP 连接不支持执行命令"}
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, cn model.SavedConnection) {
			defer func() {
				<-sem
				wg.Done()
			}()
			results[idx] = runOne(cn, command, time.Duration(timeoutSec)*time.Second)
		}(i, cn)
	}
	wg.Wait()
	return results, nil
}

func runOne(cn model.SavedConnection, command string, timeout time.Duration) types.BatchExecResult {
	res := types.BatchExecResult{ConnID: cn.ID, Name: cn.Name, Host: cn.Host}
	start := time.Now()
	defer func() { res.Duration = time.Since(start).Milliseconds() }()

	opts := sshconn.Resolve(sshconn.OptionsFromSaved(cn))
	client, err := sshlib.Dial(opts)
	if err != nil {
		res.Error = dialErrorText(err)
		return res
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		res.Error = fmt.Sprintf("创建会话失败: %v", err)
		return res
	}
	defer sess.Close()

	type outcome struct {
		out []byte
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := sess.CombinedOutput(command)
		done <- outcome{out, err}
	}()

	select {
	case oc := <-done:
		res.Output = clipOutput(string(oc.out))
		if oc.err != nil {
			var ee *xssh.ExitError
			if errors.As(oc.err, &ee) {
				res.ExitCode = ee.ExitStatus()
			} else {
				res.Error = oc.err.Error()
			}
		}
	case <-time.After(timeout):
		_ = sess.Close()
		res.Error = fmt.Sprintf("执行超时（%s）", timeout)
	}
	return res
}

func clipOutput(s string) string {
	if len(s) <= maxOutputSize {
		return s
	}
	return s[:maxOutputSize] + "\n…（输出过长，已截断）"
}

func dialErrorText(err error) string {
	if hk := sshlib.AsHostKeyError(err); hk != nil {
		return "主机密钥未信任，请先在终端中连接并信任该服务器"
	}
	return err.Error()
}