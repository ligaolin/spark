// Package rest provides a built-in HTTP client (like curl or Postman) that
// executes requests through the Go backend, bypassing browser CORS restrictions.
// It also manages a persisted request tree (multi-level folders) and named
// environments (base URL + common headers).
package rest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"spark/app/model"
	"spark/app/service/db"
	"spark/app/service/types"

	"github.com/coder/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
	"gorm.io/gorm"
)

// RestService exposes an HTTP request proxy and REST request management to the frontend.
type RestService struct{}

// ServiceName implements application.ServiceName.
func (s *RestService) ServiceName() string { return "RestService" }

// ---------------------------------------------------------------------------
// HTTP send
// ---------------------------------------------------------------------------

// Send makes an HTTP request and returns the response synchronously.
func (s *RestService) Send(req types.RestRequest) types.RestResponse {
	start := time.Now()

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    "URL 不能为空",
		}
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: req.Insecure},
		},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			return nil
		},
	}

	var bodyReader io.Reader
	var contentType string

	if len(req.FormFiles) > 0 {
		pr, pw := io.Pipe()
		writer := multipart.NewWriter(pw)

		go func() {
			defer pw.Close()
			defer writer.Close()
			for k, v := range req.FormData {
				if err := writer.WriteField(k, v); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
			for _, f := range req.FormFiles {
				part, err := writer.CreateFormFile(f.FieldName, f.FileName)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				file, err := os.Open(f.FilePath)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				if _, err := io.Copy(part, file); err != nil {
					file.Close()
					pw.CloseWithError(err)
					return
				}
				file.Close()
			}
		}()
		bodyReader = pr
		contentType = writer.FormDataContentType()
	} else if req.Body != "" {
		bodyReader = bytes.NewReader([]byte(req.Body))
	}

	httpReq, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    fmt.Sprintf("构造请求失败: %v", err),
		}
	}

	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "Spark/1.0")
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return types.RestResponse{
			Duration: time.Since(start).Milliseconds(),
			Error:    fmt.Sprintf("请求失败: %v", err),
		}
	}

	// 检测是否为流式响应
	// 1. Content-Type 显式声明为流式类型
	// 2. Transfer-Encoding: chunked（很多流式 API 用 chunked + application/json 等方式）
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	te := strings.ToLower(resp.Header.Get("Transfer-Encoding"))
	isStreaming := strings.Contains(ct, "text/event-stream") ||
		strings.Contains(ct, "application/x-ndjson") ||
		strings.Contains(ct, "application/json-seq") ||
		strings.Contains(ct, "multipart/x-mixed-replace") ||
		strings.Contains(te, "chunked")

	headers := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	if isStreaming {
		streamID := startStream(resp.Body)
		return types.RestResponse{
			Status:     resp.StatusCode,
			StatusText: resp.Status,
			Headers:    headers,
			Duration:   time.Since(start).Milliseconds(),
			Streaming:  true,
			StreamID:   streamID,
		}
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	resp.Body.Close()
	if err != nil {
		return types.RestResponse{
			Status:     resp.StatusCode,
			StatusText: resp.Status,
			Duration:   time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("读取响应体失败: %v", err),
		}
	}

	return types.RestResponse{
		Status:     resp.StatusCode,
		StatusText: resp.Status,
		Headers:    headers,
		Body:       string(bodyBytes),
		Duration:   time.Since(start).Milliseconds(),
	}
}

// ---------------------------------------------------------------------------
// Streaming helpers (SSE / event-stream)
// ---------------------------------------------------------------------------

var (
	streams   = map[string]*streamState{}
	streamsMu sync.Mutex
)

const maxStreamBuffer = 1 << 20 // 1MB

type streamState struct {
	body   io.ReadCloser
	buffer strings.Builder
	mu     sync.Mutex
	done   bool
	err    error
}

func startStream(body io.ReadCloser) string {
	id := fmt.Sprintf("stream_%d", time.Now().UnixNano())
	st := &streamState{body: body}
	streamsMu.Lock()
	streams[id] = st
	streamsMu.Unlock()

	go func() {
		defer func() {
			st.mu.Lock()
			st.done = true
			st.mu.Unlock()
			body.Close()
			time.AfterFunc(30*time.Second, func() {
				streamsMu.Lock()
				delete(streams, id)
				streamsMu.Unlock()
			})
		}()

		// 使用固定缓冲区读取，而非按行扫描，确保对各种流式响应（SSE、NDJSON、
		// chunked JSON 等）都能及时将数据交付给前端
		reader := bufio.NewReader(body)
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				st.mu.Lock()
				if st.buffer.Len() < maxStreamBuffer {
					writeN := n
					if st.buffer.Len()+n > maxStreamBuffer {
						st.buffer.Reset()
						writeN = maxStreamBuffer
					}
					st.buffer.Write(buf[:writeN])
				}
				st.mu.Unlock()
			}
			if err != nil {
				if err != io.EOF {
					st.mu.Lock()
					st.err = err
					st.mu.Unlock()
				}
				break
			}
		}
	}()

	return id
}

// ReadStreamChunks polls accumulated stream data. Call repeatedly until Done.
func (s *RestService) ReadStreamChunks(streamID string) types.StreamChunk {
	streamsMu.Lock()
	st, ok := streams[streamID]
	streamsMu.Unlock()
	if !ok {
		return types.StreamChunk{Done: true}
	}

	st.mu.Lock()
	data := st.buffer.String()
	st.buffer.Reset()
	done := st.done
	err := st.err
	st.mu.Unlock()

	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	return types.StreamChunk{
		Data:  data,
		Done:  done,
		Error: errStr,
	}
}

// ---------------------------------------------------------------------------
// Tree: ListChildren (lazy-load one level)
// ---------------------------------------------------------------------------

// ListChildren returns direct children of parentID (0 = root).
// It returns a flat list of mixed folder + request nodes.
func (s *RestService) ListChildren(parentID uint) ([]types.RestNode, error) {
	var result []types.RestNode

	// Folders
	var folders []model.RestFolder
	if err := db.GetDB().Where("parent_id = ?", parentID).Order("sort asc, name asc").Find(&folders).Error; err != nil {
		return nil, err
	}
	for _, f := range folders {
		result = append(result, types.RestNode{
			ID:            f.ID,
			ParentID:      f.ParentID,
			Name:          f.Name,
			Type:          "folder",
			BaseURL:       f.BaseURL,
			CommonHeaders: f.CommonHeaders,
			Leaf:          false,
			Sort:          f.Sort,
		})
	}

	// Requests
	var reqs []model.RestRequestModel
	if err := db.GetDB().Where("folder_id = ?", parentID).Order("sort asc, name asc").Find(&reqs).Error; err != nil {
		return nil, err
	}
	for _, r := range reqs {
		result = append(result, types.RestNode{
			ID:       r.ID,
			ParentID: r.FolderID,
			Name:     r.Name,
			Type:     "request",
			Method:   r.Method,
			URL:      r.URL,
			BaseURL:  r.BaseURL,
			Leaf:     true,
			Sort:     r.Sort,
		})
	}

	if result == nil {
		result = []types.RestNode{}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Folder CRUD
// ---------------------------------------------------------------------------

// CreateFolder creates a new folder under parentID (0 = root).
func (s *RestService) CreateFolder(parentID uint, name string) (model.RestFolder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.RestFolder{}, errors.New("文件夹名称不能为空")
	}
	if err := checkFolderNameConflict(parentID, name); err != nil {
		return model.RestFolder{}, err
	}
	// next sort
	var maxSort int
	db.GetDB().Model(&model.RestFolder{}).Where("parent_id = ?", parentID).
		Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
	n := model.RestFolder{
		ParentID: parentID,
		Name:     name,
		Sort:     maxSort + 1,
	}
	if err := db.GetDB().Create(&n).Error; err != nil {
		return n, err
	}
	return n, nil
}

// RenameFolder renames a folder.
func (s *RestService) RenameFolder(id uint, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("名称不能为空")
	}
	var f model.RestFolder
	if err := db.GetDB().First(&f, id).Error; err != nil {
		return err
	}
	if name != f.Name {
		if err := checkFolderNameConflict(f.ParentID, name); err != nil {
			return err
		}
	}
	return db.GetDB().Model(&model.RestFolder{}).Where("id = ?", id).Update("name", name).Error
}

// ---------------------------------------------------------------------------
// Request CRUD
// ---------------------------------------------------------------------------

// CreateRequest creates a new request under a folder (folderID 0 = root level).
func (s *RestService) CreateRequest(folderID uint, name string) (model.RestRequestModel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.RestRequestModel{}, errors.New("请求名称不能为空")
	}
	if err := checkRequestNameConflict(folderID, name); err != nil {
		return model.RestRequestModel{}, err
	}
	var maxSort int
	db.GetDB().Model(&model.RestRequestModel{}).Where("folder_id = ?", folderID).
		Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
	n := model.RestRequestModel{
		FolderID: folderID,
		Name:     name,
		Method:   "GET",
		Headers:  "[]",
		Params:   "[]",
		Sort:     maxSort + 1,
	}
	if err := db.GetDB().Create(&n).Error; err != nil {
		return n, err
	}
	return n, nil
}

// GetRequest returns the full content of a saved request.
func (s *RestService) GetRequest(id uint) (types.RestItem, error) {
	var r model.RestRequestModel
	if err := db.GetDB().First(&r, id).Error; err != nil {
		return types.RestItem{}, err
	}
	return modelToItem(&r), nil
}

// SaveRequest creates or updates a request.
func (s *RestService) SaveRequest(req types.RestSaveRequest) (types.RestItem, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return types.RestItem{}, errors.New("请求名称不能为空")
	}
	headersJSON, err := json.Marshal(req.Headers)
	if err != nil {
		return types.RestItem{}, fmt.Errorf("序列化请求头失败: %w", err)
	}
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return types.RestItem{}, fmt.Errorf("序列化查询参数失败: %w", err)
	}

	if req.ID == 0 {
		// Create new
		if err := checkRequestNameConflict(req.FolderID, req.Name); err != nil {
			return types.RestItem{}, err
		}
		var maxSort int
		db.GetDB().Model(&model.RestRequestModel{}).Where("folder_id = ?", req.FolderID).
			Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
		m := model.RestRequestModel{
			FolderID: req.FolderID,
			Name:     req.Name,
			Method:   req.Method,
			URL:      req.URL,
			BaseURL:  req.BaseURL,
			Headers:  string(headersJSON),
			Params:   string(paramsJSON),
			Body:     req.Body,
			Sort:     maxSort + 1,
		}
		if err := db.GetDB().Create(&m).Error; err != nil {
			return types.RestItem{}, err
		}
		return modelToItem(&m), nil
	}

	// Update existing
	var m model.RestRequestModel
	if err := db.GetDB().First(&m, req.ID).Error; err != nil {
		return types.RestItem{}, err
	}
	if req.Name != m.Name {
		if err := checkRequestNameConflict(m.FolderID, req.Name); err != nil {
			return types.RestItem{}, err
		}
	}
	m.FolderID = req.FolderID
	m.Name = req.Name
	m.Method = req.Method
	m.URL = req.URL
	m.BaseURL = req.BaseURL
	m.Headers = string(headersJSON)
	m.Params = string(paramsJSON)
	m.Body = req.Body
	if err := db.GetDB().Save(&m).Error; err != nil {
		return types.RestItem{}, err
	}
	return modelToItem(&m), nil
}

// RenameRequest renames a request.
func (s *RestService) RenameRequest(id uint, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("名称不能为空")
	}
	var r model.RestRequestModel
	if err := db.GetDB().First(&r, id).Error; err != nil {
		return err
	}
	if name != r.Name {
		if err := checkRequestNameConflict(r.FolderID, name); err != nil {
			return err
		}
	}
	return db.GetDB().Model(&model.RestRequestModel{}).Where("id = ?", id).Update("name", name).Error
}

// ---------------------------------------------------------------------------
// Delete (recursive for folders)
// ---------------------------------------------------------------------------

// DeleteNode deletes a folder (recursively) or a request.
func (s *RestService) DeleteNode(id uint, nodeType string) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	if nodeType == "folder" {
		return deleteFolderTree(id)
	}
	return db.GetDB().Delete(&model.RestRequestModel{}, id).Error
}

func deleteFolderTree(folderID uint) error {
	// Collect all descendant folder IDs
	var folderIDs []uint
	var queue []uint
	queue = append(queue, folderID)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		folderIDs = append(folderIDs, id)
		var children []model.RestFolder
		if err := db.GetDB().Where("parent_id = ?", id).Find(&children).Error; err != nil {
			return err
		}
		for _, c := range children {
			queue = append(queue, c.ID)
		}
	}
	// Delete requests in all these folders
	if err := db.GetDB().Where("folder_id IN ?", folderIDs).Delete(&model.RestRequestModel{}).Error; err != nil {
		return err
	}
	// Delete folders
	return db.GetDB().Where("id IN ?", folderIDs).Delete(&model.RestFolder{}).Error
}

// ---------------------------------------------------------------------------
// Move / Reorder
// ---------------------------------------------------------------------------

// MoveNode moves a folder or request to a new parent (newParentID = 0 for root).
// Node type: "folder" for RestFolder, "request" for RestRequestModel.
func (s *RestService) MoveNode(id uint, nodeType string, newParentID uint, targetID uint, position string) error {
	if id == 0 {
		return errors.New("节点 ID 不能为空")
	}
	if nodeType == "folder" {
		return moveFolder(id, newParentID, targetID, position)
	}
	return moveRequest(id, newParentID, targetID, position)
}

func moveFolder(id uint, newParentID uint, targetID uint, position string) error {
	var f model.RestFolder
	if err := db.GetDB().First(&f, id).Error; err != nil {
		return err
	}
	if f.ParentID != newParentID {
		if id == newParentID {
			return errors.New("不能移动到自身")
		}
		if isFolderDescendant(id, newParentID) {
			return errors.New("不能移动到自己的子目录中")
		}
		if err := db.GetDB().Model(&model.RestFolder{}).Where("id = ?", id).Update("parent_id", newParentID).Error; err != nil {
			return err
		}
	}
	return reorderFolders(newParentID, id, targetID, position)
}

func moveRequest(id uint, newParentID uint, targetID uint, position string) error {
	var r model.RestRequestModel
	if err := db.GetDB().First(&r, id).Error; err != nil {
		return err
	}
	if r.FolderID != newParentID {
		if err := db.GetDB().Model(&model.RestRequestModel{}).Where("id = ?", id).Update("folder_id", newParentID).Error; err != nil {
			return err
		}
	}
	return reorderRequests(newParentID, id, targetID, position)
}

func reorderFolders(parentID uint, selfID uint, targetID uint, position string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var siblings []model.RestFolder
		if err := tx.Where("parent_id = ?", parentID).Order("sort asc, id asc").Find(&siblings).Error; err != nil {
			return err
		}
		ids := make([]uint, 0, len(siblings))
		for _, sib := range siblings {
			if sib.ID == selfID {
				continue
			}
			ids = append(ids, sib.ID)
		}
		pos := len(ids)
		if targetID != 0 {
			for i, sid := range ids {
				if sid == targetID {
					pos = i
					if position == "after" {
						pos = i + 1
					}
					break
				}
			}
		}
		ids = append(ids, 0)
		copy(ids[pos+1:], ids[pos:])
		ids[pos] = selfID
		for i, sid := range ids {
			if err := tx.Model(&model.RestFolder{}).Where("id = ?", sid).Update("sort", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func reorderRequests(parentID uint, selfID uint, targetID uint, position string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var siblings []model.RestRequestModel
		if err := tx.Where("folder_id = ?", parentID).Order("sort asc, id asc").Find(&siblings).Error; err != nil {
			return err
		}
		ids := make([]uint, 0, len(siblings))
		for _, sib := range siblings {
			if sib.ID == selfID {
				continue
			}
			ids = append(ids, sib.ID)
		}
		pos := len(ids)
		if targetID != 0 {
			for i, sid := range ids {
				if sid == targetID {
					pos = i
					if position == "after" {
						pos = i + 1
					}
					break
				}
			}
		}
		ids = append(ids, 0)
		copy(ids[pos+1:], ids[pos:])
		ids[pos] = selfID
		for i, sid := range ids {
			if err := tx.Model(&model.RestRequestModel{}).Where("id = ?", sid).Update("sort", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func isFolderDescendant(parentID uint, childID uint) bool {
	stepOnce := func(id uint) (uint, bool) {
		var f model.RestFolder
		if err := db.GetDB().Select("parent_id").First(&f, id).Error; err != nil {
			return 0, false
		}
		return f.ParentID, true
	}

	slow, ok := stepOnce(childID)
	if !ok || slow == 0 {
		return false
	}
	if slow == parentID {
		return true
	}

	fast := slow
	for {
		var ok1, ok2 bool
		fast, ok1 = stepOnce(fast)
		if !ok1 || fast == 0 {
			return false
		}
		if fast == parentID {
			return true
		}
		fast, ok2 = stepOnce(fast)
		if !ok2 || fast == 0 {
			return false
		}
		if fast == parentID {
			return true
		}
		slow, ok = stepOnce(slow)
		if !ok || slow == 0 {
			return false
		}
		if slow == fast {
			return false
		}
	}
}

// ---------------------------------------------------------------------------
// Environments
// ---------------------------------------------------------------------------

// ListEnvironments returns all saved environments.
func (s *RestService) ListEnvironments() ([]types.RestEnvItem, error) {
	var list []model.RestEnvironment
	if err := db.GetDB().Order("sort asc, name asc").Find(&list).Error; err != nil {
		return nil, err
	}
	result := make([]types.RestEnvItem, 0, len(list))
	for _, e := range list {
		result = append(result, envToItem(&e))
	}
	return result, nil
}

// SaveEnvironment creates or updates an environment.
func (s *RestService) SaveEnvironment(env types.RestSaveEnv) (types.RestEnvItem, error) {
	env.Name = strings.TrimSpace(env.Name)
	if env.Name == "" {
		return types.RestEnvItem{}, errors.New("环境名称不能为空")
	}
	headersJSON, err := json.Marshal(env.CommonHeaders)
	if err != nil {
		return types.RestEnvItem{}, fmt.Errorf("序列化公共请求头失败: %w", err)
	}

	if env.ID == 0 {
		// Create new
		var maxSort int
		db.GetDB().Model(&model.RestEnvironment{}).Select("COALESCE(MAX(sort), -1)").Scan(&maxSort)
		m := model.RestEnvironment{
			Name:          env.Name,
			BaseURL:       env.BaseURL,
			CommonHeaders: string(headersJSON),
			IsDefault:     env.IsDefault,
			Sort:          maxSort + 1,
		}
		if env.IsDefault {
			db.GetDB().Model(&model.RestEnvironment{}).Where("is_default = ?", true).Update("is_default", false)
		}
		if err := db.GetDB().Create(&m).Error; err != nil {
			return types.RestEnvItem{}, err
		}
		return envToItem(&m), nil
	}

	// Update
	var m model.RestEnvironment
	if err := db.GetDB().First(&m, env.ID).Error; err != nil {
		return types.RestEnvItem{}, err
	}
	if env.IsDefault && !m.IsDefault {
		db.GetDB().Model(&model.RestEnvironment{}).Where("is_default = ?", true).Update("is_default", false)
	}
	m.Name = env.Name
	m.BaseURL = env.BaseURL
	m.CommonHeaders = string(headersJSON)
	m.IsDefault = env.IsDefault
	if err := db.GetDB().Save(&m).Error; err != nil {
		return types.RestEnvItem{}, err
	}
	return envToItem(&m), nil
}

// DeleteEnvironment deletes an environment.
func (s *RestService) DeleteEnvironment(id uint) error {
	if id == 0 {
		return errors.New("环境 ID 不能为空")
	}
	return db.GetDB().Delete(&model.RestEnvironment{}, id).Error
}

// SetDefaultEnvironment sets the default environment.
func (s *RestService) SetDefaultEnvironment(id uint) error {
	db.GetDB().Model(&model.RestEnvironment{}).Where("is_default = ?", true).Update("is_default", false)
	if id > 0 {
		return db.GetDB().Model(&model.RestEnvironment{}).Where("id = ?", id).Update("is_default", true).Error
	}
	return nil
}

// SetFolderBaseURL sets a folder's base URL. When empty, inherits from parent
// folder or global environment.
func (s *RestService) SetFolderBaseURL(id uint, baseURL string) error {
	if id == 0 {
		return errors.New("文件夹 ID 不能为空")
	}
	return db.GetDB().Model(&model.RestFolder{}).Where("id = ?", id).Update("base_url", baseURL).Error
}

// SetRequestBaseURL sets a request's own base URL override.
func (s *RestService) SetRequestBaseURL(id uint, baseURL string) error {
	if id == 0 {
		return errors.New("请求 ID 不能为空")
	}
	return db.GetDB().Model(&model.RestRequestModel{}).Where("id = ?", id).Update("base_url", baseURL).Error
}

// GetEffectiveBaseURL walks up the folder hierarchy and returns the nearest
// non-empty BaseURL. Returns empty string if no folder in the chain has a
// BaseURL set.
func (s *RestService) GetEffectiveBaseURL(folderID uint) string {
	return getEffectiveBaseURL(folderID)
}

// SetFolderCommonHeaders sets a folder's common headers (JSON string).
func (s *RestService) SetFolderCommonHeaders(id uint, headersJSON string) error {
	if id == 0 {
		return errors.New("文件夹 ID 不能为空")
	}
	return db.GetDB().Model(&model.RestFolder{}).Where("id = ?", id).Update("common_headers", headersJSON).Error
}

// GetEffectiveCommonHeaders walks up the folder hierarchy and returns merged
// common headers (closer folders override ancestor keys).
func (s *RestService) GetEffectiveCommonHeaders(folderID uint) []types.KV {
	return getEffectiveCommonHeaders(folderID)
}

// ---------------------------------------------------------------------------
// Stress test
// ---------------------------------------------------------------------------

// StressTest runs a concurrent stress test with the given request.
func (s *RestService) StressTest(req types.StressTestRequest) types.StressTestResult {
	if req.Concurrency <= 0 {
		req.Concurrency = 1
	}
	if req.Total <= 0 {
		req.Total = 1
	}
	if req.Timeout <= 0 {
		req.Timeout = 10
	}

	type record struct {
		statusCode int
		duration   int64
		success    bool
	}

	var mu sync.Mutex
	records := make([]record, 0, req.Total)

	var counter int32
	var wg sync.WaitGroup

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}

	client := &http.Client{
		Timeout: time.Duration(req.Timeout) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: req.Insecure},
		},
	}

	start := time.Now()

	for i := 0; i < req.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := atomic.AddInt32(&counter, 1)
				if int(idx) > req.Total {
					return
				}
				t0 := time.Now()

				var bodyReader io.Reader
				if req.Body != "" {
					bodyReader = strings.NewReader(req.Body)
				}

				httpReq, err := http.NewRequest(method, req.URL, bodyReader)
				if err != nil {
					mu.Lock()
					records = append(records, record{statusCode: 0, duration: time.Since(t0).Milliseconds(), success: false})
					mu.Unlock()
					continue
				}

				for k, v := range req.Headers {
					httpReq.Header.Set(k, v)
				}
				if httpReq.Header.Get("User-Agent") == "" {
					httpReq.Header.Set("User-Agent", "Spark/1.0")
				}

				resp, err := client.Do(httpReq)
				dur := time.Since(t0).Milliseconds()
				if err != nil {
					mu.Lock()
					records = append(records, record{statusCode: 0, duration: dur, success: false})
					mu.Unlock()
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				mu.Lock()
				records = append(records, record{statusCode: resp.StatusCode, duration: dur, success: resp.StatusCode < 500})
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start).Milliseconds()

	// Statistics
	var successCount int64
	var failureCount int64
	statuses := map[string]int64{}
	var latencies []int64

	mu.Lock()
	for _, r := range records {
		if r.success {
			successCount++
		} else {
			failureCount++
		}
		statusKey := fmt.Sprintf("%d", r.statusCode)
		if r.statusCode == 0 {
			statusKey = "error"
		}
		statuses[statusKey]++
		latencies = append(latencies, r.duration)
	}
	mu.Unlock()

	// Sort latencies for percentile calculation
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	n := len(latencies)
	var min, max, sum, p50, p95, p99 int64
	if n > 0 {
		min = latencies[0]
		max = latencies[n-1]
		for _, v := range latencies {
			sum += v
		}
		p50 = latencies[n*50/100]
		p95Idx := n * 95 / 100
		if p95Idx >= n {
			p95Idx = n - 1
		}
		p95 = latencies[p95Idx]
		p99Idx := n * 99 / 100
		if p99Idx >= n {
			p99Idx = n - 1
		}
		p99 = latencies[p99Idx]
	}
	var avg int64
	if n > 0 {
		avg = sum / int64(n)
	}

	var qps float64
	if elapsed > 0 {
		qps = float64(req.Total) / (float64(elapsed) / 1000.0)
	}

	return types.StressTestResult{
		Total:    int64(req.Total),
		Success:  successCount,
		Failure:  failureCount,
		Duration: elapsed,
		QPS:      math.Round(qps*100) / 100,
		Latency: types.StressTestLatency{
			Min: min,
			Max: max,
			Avg: avg,
			P50: p50,
			P95: p95,
			P99: p99,
		},
		Statuses: statuses,
	}
}

// ---------------------------------------------------------------------------
// WebSocket client
// ---------------------------------------------------------------------------

var (
	wsConns      = map[string]*websocket.Conn{}
	wsLastActive = map[string]time.Time{}
	wsMu         sync.Mutex
	wsCleanOnce  sync.Once
)

func startWSCleaner() {
	wsCleanOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				wsMu.Lock()
				for id, last := range wsLastActive {
					if time.Since(last) > 30*time.Minute {
						if conn, ok := wsConns[id]; ok {
							_ = conn.Close(websocket.StatusNormalClosure, "idle timeout")
							delete(wsConns, id)
						}
						delete(wsLastActive, id)
					}
				}
				wsMu.Unlock()
			}
		}()
	})
}

// WSConnect dials a WebSocket server and returns a connection id.
// Incoming messages are pushed directly to the frontend via the
// "rest:ws:message" event (payload: {connId, type, data, time, sent}).
func (s *RestService) WSConnect(ctx context.Context, req types.WSConnectRequest) types.WSConnectResult {
	startWSCleaner()

	h := http.Header{}
	for k, v := range req.Headers {
		h.Set(k, v)
	}

	conn, resp, err := websocket.Dial(ctx, req.URL, &websocket.DialOptions{
		HTTPHeader: h,
	})
	if err != nil {
		return types.WSConnectResult{Error: err.Error()}
	}

	hdrs := map[string]string{}
	if resp != nil {
		for k, vs := range resp.Header {
			hdrs[k] = strings.Join(vs, ", ")
		}
	}

	id := fmt.Sprintf("ws_%d", time.Now().UnixNano())
	wsMu.Lock()
	wsConns[id] = conn
	wsLastActive[id] = time.Now()
	wsMu.Unlock()

	go func() {
		defer func() {
			wsMu.Lock()
			delete(wsConns, id)
			delete(wsLastActive, id)
			wsMu.Unlock()
		}()
		for {
			_, data, err := conn.Read(ctx)
			wsMu.Lock()
			wsLastActive[id] = time.Now()
			wsMu.Unlock()

			msg := types.WSMessage{Time: time.Now().UnixMilli(), Sent: false}
			if err != nil {
				msg.Type = "close"
				msg.Data = err.Error()
			} else {
				msg.Type = "text"
				msg.Data = string(data)
			}

			application.Get().Event.Emit("rest:ws:message", map[string]any{
				"connId": id,
				"type":   msg.Type,
				"data":   msg.Data,
				"time":   msg.Time,
				"sent":   msg.Sent,
			})

			if err != nil {
				return
			}
		}
	}()

	return types.WSConnectResult{ConnID: id, Headers: hdrs}
}

// WSSend sends a text message on an open WebSocket connection.
func (s *RestService) WSSend(ctx context.Context, connID string, message string) error {
	wsMu.Lock()
	conn, ok := wsConns[connID]
	if ok {
		wsLastActive[connID] = time.Now()
	}
	wsMu.Unlock()

	if !ok {
		return fmt.Errorf("connection not found")
	}

	err := conn.Write(ctx, websocket.MessageText, []byte(message))

	application.Get().Event.Emit("rest:ws:message", map[string]any{
		"connId": connID,
		"type":   "text",
		"data":   message,
		"time":   time.Now().UnixMilli(),
		"sent":   true,
	})

	return err
}

// WSClose closes a WebSocket connection.
func (s *RestService) WSClose(ctx context.Context, connID string) error {
	wsMu.Lock()
	conn, ok := wsConns[connID]
	delete(wsConns, connID)
	delete(wsLastActive, connID)
	wsMu.Unlock()

	if !ok {
		return fmt.Errorf("connection not found")
	}

	return conn.Close(websocket.StatusNormalClosure, "client closed")
}

// WSReadMessages is kept for backwards compatibility with bindings generated
// before the event-push refactor. Incoming messages are now delivered via
// the "rest:ws:message" event, so this always returns an empty result.
func (s *RestService) WSReadMessages(connID string) types.WSReadResult {
	return types.WSReadResult{Messages: []types.WSMessage{}}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func checkFolderNameConflict(parentID uint, name string) error {
	var count int64
	if err := db.GetDB().Model(&model.RestFolder{}).
		Where("parent_id = ? AND name = ?", parentID, name).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同级目录下已存在同名文件夹")
	}
	return nil
}

func checkRequestNameConflict(folderID uint, name string) error {
	var count int64
	if err := db.GetDB().Model(&model.RestRequestModel{}).
		Where("folder_id = ? AND name = ?", folderID, name).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("同级目录下已存在同名请求")
	}
	return nil
}

func modelToItem(m *model.RestRequestModel) types.RestItem {
	var headers []types.KV
	json.Unmarshal([]byte(m.Headers), &headers)
	if headers == nil {
		headers = []types.KV{}
	}
	var params []types.KV
	json.Unmarshal([]byte(m.Params), &params)
	if params == nil {
		params = []types.KV{}
	}
	effectiveBaseURL := getEffectiveBaseURL(m.FolderID)
	folderCommonHeaders := getEffectiveCommonHeaders(m.FolderID)
	return types.RestItem{
		ID:                  m.ID,
		FolderID:            m.FolderID,
		Name:                m.Name,
		Method:              m.Method,
		URL:                 m.URL,
		BaseURL:             m.BaseURL,
		EffectiveBaseURL:    effectiveBaseURL,
		FolderCommonHeaders: folderCommonHeaders,
		Headers:             headers,
		Params:              params,
		Body:                m.Body,
	}
}

func envToItem(e *model.RestEnvironment) types.RestEnvItem {
	var headers []types.KV
	json.Unmarshal([]byte(e.CommonHeaders), &headers)
	if headers == nil {
		headers = []types.KV{}
	}
	return types.RestEnvItem{
		ID:            e.ID,
		Name:          e.Name,
		BaseURL:       e.BaseURL,
		CommonHeaders: headers,
		IsDefault:     e.IsDefault,
		Sort:          e.Sort,
	}
}

// getEffectiveBaseURL walks up the folder hierarchy and returns the nearest
// non-empty BaseURL.
func getEffectiveBaseURL(folderID uint) string {
	if folderID == 0 {
		return ""
	}
	for {
		var f model.RestFolder
		if err := db.GetDB().Select("id, parent_id, base_url").First(&f, folderID).Error; err != nil {
			return ""
		}
		if f.BaseURL != "" {
			return f.BaseURL
		}
		if f.ParentID == 0 {
			return ""
		}
		folderID = f.ParentID
	}
}

// getEffectiveCommonHeaders walks up the folder hierarchy from leaf to root,
// collecting commonHeaders. Closer folders override earlier (ancestor) keys.
func getEffectiveCommonHeaders(folderID uint) []types.KV {
	var allFolders []model.RestFolder
	cur := folderID
	for cur != 0 {
		var f model.RestFolder
		if err := db.GetDB().Select("id, parent_id, common_headers").First(&f, cur).Error; err != nil {
			break
		}
		allFolders = append(allFolders, f)
		cur = f.ParentID
	}
	// Reverse so ancestors come first, then children override
	result := make(map[string]string)
	for i := len(allFolders) - 1; i >= 0; i-- {
		var headers []types.KV
		if err := json.Unmarshal([]byte(allFolders[i].CommonHeaders), &headers); err != nil {
			continue
		}
		for _, h := range headers {
			k := strings.TrimSpace(h.Key)
			if k != "" {
				result[k] = h.Value
			}
		}
	}
	out := make([]types.KV, 0, len(result))
	for k, v := range result {
		out = append(out, types.KV{Key: k, Value: v})
	}
	return out
}
