package transfer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/landrop/landrop/backend/internal/config"
	"github.com/landrop/landrop/backend/internal/events"
	"github.com/landrop/landrop/backend/internal/model"
	"github.com/landrop/landrop/backend/internal/store"
	"github.com/landrop/landrop/backend/internal/transport"
)

type Request struct {
	TargetID string   `json:"targetId"`
	Files    []string `json:"files"`
}

type Job struct {
	ID               string     `json:"id"`
	TargetID         string     `json:"targetId"`
	TargetName       string     `json:"targetName,omitempty"`
	Direction        string     `json:"direction"`
	Files            []string   `json:"files"`
	Status           string     `json:"status"`
	Message          string     `json:"message,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	BytesTotal       int64      `json:"bytesTotal"`
	BytesTransferred int64      `json:"bytesTransferred"`
	CurrentFile      string     `json:"currentFile,omitempty"`
	ReceiveDir       string     `json:"receiveDir,omitempty"`
	Error            string     `json:"error,omitempty"`
}

type Manager struct {
	config    config.Config
	devices   *store.DeviceRegistry
	publisher events.Publisher
}

type fileSpec struct {
	Path string
	Name string
	Size int64
}

type peerFrame struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OS   string `json:"os"`
}

type helloFrame struct {
	Type       string    `json:"type"`
	JobID      string    `json:"jobId"`
	Sender     peerFrame `json:"sender"`
	FileCount  int       `json:"fileCount"`
	TotalBytes int64     `json:"totalBytes"`
}

type fileFrame struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type doneFrame struct {
	Type  string `json:"type"`
	JobID string `json:"jobId"`
}

type acceptFrame struct {
	Type       string `json:"type"`
	JobID      string `json:"jobId,omitempty"`
	ReceiveDir string `json:"receiveDir,omitempty"`
	Message    string `json:"message,omitempty"`
}

type errorFrame struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type frameKind struct {
	Type string `json:"type"`
}

func NewManager(cfg config.Config, devices *store.DeviceRegistry, publisher events.Publisher) *Manager {
	return &Manager{
		config:    cfg,
		devices:   devices,
		publisher: publisher,
	}
}

func (m *Manager) Queue(request Request) (Job, error) {
	if strings.TrimSpace(request.TargetID) == "" {
		return Job{}, errors.New("targetId is required")
	}

	if len(request.Files) == 0 {
		return Job{}, errors.New("at least one file is required")
	}

	target, ok := m.devices.Get(strings.TrimSpace(request.TargetID))
	if !ok {
		return Job{}, errors.New("target device is offline or unknown")
	}

	files, names, totalBytes, err := m.prepareFiles(request.Files)
	if err != nil {
		return Job{}, err
	}

	job := Job{
		ID:         fmt.Sprintf("job-%d", time.Now().UnixNano()),
		TargetID:   target.ID,
		TargetName: target.Name,
		Direction:  "outgoing",
		Files:      names,
		Status:     "queued",
		Message:    fmt.Sprintf("已排队，准备发送到 %s", target.Name),
		CreatedAt:  time.Now().UTC(),
		BytesTotal: totalBytes,
	}

	m.publishJob("transfer.queued", job)

	go m.runOutgoing(job, target, files)

	return job, nil
}

func (m *Manager) HandleIncoming(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	raw, err := readFrameBytes(reader)
	if err != nil {
		return
	}

	var hello helloFrame
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Type != "hello" || hello.JobID == "" {
		_ = writeFrame(conn, errorFrame{Type: "error", Message: "invalid hello frame"})
		return
	}

	if hello.FileCount <= 0 || hello.TotalBytes < 0 {
		_ = writeFrame(conn, errorFrame{Type: "error", Message: "invalid transfer metadata"})
		return
	}

	receiveRoot, err := m.prepareReceiveRoot(hello.Sender.Name, hello.JobID)
	if err != nil {
		_ = writeFrame(conn, errorFrame{Type: "error", Message: err.Error()})
		return
	}

	startedAt := time.Now().UTC()
	job := Job{
		ID:         hello.JobID,
		TargetID:   hello.Sender.ID,
		TargetName: hello.Sender.Name,
		Direction:  "incoming",
		Files:      make([]string, 0, hello.FileCount),
		Status:     "running",
		Message:    fmt.Sprintf("正在接收来自 %s 的文件", hello.Sender.Name),
		CreatedAt:  startedAt,
		StartedAt:  &startedAt,
		BytesTotal: hello.TotalBytes,
		ReceiveDir: receiveRoot,
	}

	m.publishJob("transfer.incoming", job)
	_ = conn.SetDeadline(time.Time{})

	if err := writeFrame(conn, acceptFrame{Type: "accept", JobID: job.ID, ReceiveDir: receiveRoot, Message: "ready"}); err != nil {
		m.failJob(job, "无法确认接收会话", err)
		return
	}

	buffer := make([]byte, m.chunkSize())
	lastPublished := time.Now().Add(-time.Second)

	for {
		select {
		case <-ctx.Done():
			m.failJob(job, "接收会话被中断", ctx.Err())
			return
		default:
		}

		raw, err := readFrameBytes(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				m.failJob(job, "对端在传输完成前断开连接", err)
				return
			}

			m.failJob(job, "读取传输帧失败", err)
			return
		}

		var kind frameKind
		if err := json.Unmarshal(raw, &kind); err != nil {
			m.failJob(job, "解析传输帧失败", err)
			return
		}

		switch kind.Type {
		case "file":
			var header fileFrame
			if err := json.Unmarshal(raw, &header); err != nil {
				m.failJob(job, "解析文件头失败", err)
				return
			}

			if header.Size < 0 {
				_ = writeFrame(conn, errorFrame{Type: "error", Message: "invalid file size"})
				m.failJob(job, "收到非法文件大小", fmt.Errorf("invalid file size %d", header.Size))
				return
			}

			path, displayName, err := m.resolveReceivePath(receiveRoot, header.Name)
			if err != nil {
				_ = writeFrame(conn, errorFrame{Type: "error", Message: err.Error()})
				m.failJob(job, "准备接收文件失败", err)
				return
			}

			file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				_ = writeFrame(conn, errorFrame{Type: "error", Message: err.Error()})
				m.failJob(job, "无法创建接收文件", err)
				return
			}

			baseTransferred := job.BytesTransferred
			job.Files = append(job.Files, displayName)
			job.CurrentFile = displayName
			job.Message = fmt.Sprintf("正在接收 %s", displayName)
			m.publishJob("transfer.progress", job)

			copyErr := copyExact(file, reader, header.Size, buffer, func(written int64) {
				job.BytesTransferred = baseTransferred + written
				if time.Since(lastPublished) >= 250*time.Millisecond || job.BytesTransferred == job.BytesTotal {
					m.publishJob("transfer.progress", job)
					lastPublished = time.Now()
				}
			})
			closeErr := file.Close()
			if copyErr != nil {
				_ = os.Remove(path)
				m.failJob(job, "接收文件内容失败", copyErr)
				return
			}

			if closeErr != nil {
				m.failJob(job, "关闭接收文件失败", closeErr)
				return
			}

			job.BytesTransferred = baseTransferred + header.Size
			job.Message = fmt.Sprintf("已接收 %s", displayName)
			m.publishJob("transfer.progress", job)
		case "done":
			completedAt := time.Now().UTC()
			job.Status = "completed"
			job.BytesTransferred = job.BytesTotal
			job.Message = fmt.Sprintf("已接收 %d 个文件", len(job.Files))
			job.CurrentFile = ""
			job.CompletedAt = &completedAt

			if err := writeFrame(conn, acceptFrame{Type: "complete", JobID: job.ID, ReceiveDir: receiveRoot, Message: "transfer complete"}); err != nil {
				m.failJob(job, "无法确认接收完成", err)
				return
			}

			m.publishJob("transfer.completed", job)
			return
		default:
			_ = writeFrame(conn, errorFrame{Type: "error", Message: "unsupported frame type"})
			m.failJob(job, "收到未知传输帧", fmt.Errorf("unsupported frame type %q", kind.Type))
			return
		}
	}
}

func (m *Manager) runOutgoing(job Job, target model.Device, files []fileSpec) {
	startedAt := time.Now().UTC()
	job.Status = "connecting"
	job.StartedAt = &startedAt
	job.Message = fmt.Sprintf("正在连接 %s", target.Name)
	m.publishJob("transfer.started", job)

	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, err := transport.Dial(dialCtx, m.config, target)
	cancel()
	if err != nil {
		m.failJob(job, fmt.Sprintf("连接 %s 失败", target.Name), err)
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	if err := writeFrame(conn, helloFrame{
		Type:       "hello",
		JobID:      job.ID,
		Sender:     peerFrame{ID: m.config.DeviceID, Name: m.config.DeviceName, OS: m.config.DeviceOS},
		FileCount:  len(files),
		TotalBytes: job.BytesTotal,
	}); err != nil {
		m.failJob(job, "发送会话握手失败", err)
		return
	}

	response, err := readFrameBytes(reader)
	if err != nil {
		m.failJob(job, "等待接收端确认失败", err)
		return
	}

	var accept acceptFrame
	if err := json.Unmarshal(response, &accept); err != nil || accept.Type != "accept" {
		m.failJob(job, "接收端未确认传输请求", decodeFrameError(response, err))
		return
	}

	job.Status = "running"
	job.ReceiveDir = accept.ReceiveDir
	job.Message = fmt.Sprintf("正在发送到 %s", target.Name)
	_ = conn.SetDeadline(time.Time{})
	m.publishJob("transfer.progress", job)

	buffer := make([]byte, m.chunkSize())
	lastPublished := time.Now().Add(-time.Second)

	for _, file := range files {
		if err := writeFrame(conn, fileFrame{Type: "file", Name: file.Name, Size: file.Size}); err != nil {
			m.failJob(job, "发送文件头失败", err)
			return
		}

		handle, err := os.Open(file.Path)
		if err != nil {
			m.failJob(job, fmt.Sprintf("打开文件 %s 失败", file.Name), err)
			return
		}

		baseTransferred := job.BytesTransferred
		job.CurrentFile = file.Name
		job.Message = fmt.Sprintf("正在发送 %s", file.Name)
		m.publishJob("transfer.progress", job)

		copyErr := copyExact(conn, handle, file.Size, buffer, func(written int64) {
			job.BytesTransferred = baseTransferred + written
			if time.Since(lastPublished) >= 250*time.Millisecond || job.BytesTransferred == job.BytesTotal {
				m.publishJob("transfer.progress", job)
				lastPublished = time.Now()
			}
		})
		closeErr := handle.Close()
		if copyErr != nil {
			m.failJob(job, fmt.Sprintf("发送文件 %s 失败", file.Name), copyErr)
			return
		}

		if closeErr != nil {
			m.failJob(job, fmt.Sprintf("关闭文件 %s 失败", file.Name), closeErr)
			return
		}

		job.BytesTransferred = baseTransferred + file.Size
		job.Message = fmt.Sprintf("已发送 %s", file.Name)
		m.publishJob("transfer.progress", job)
	}

	if err := writeFrame(conn, doneFrame{Type: "done", JobID: job.ID}); err != nil {
		m.failJob(job, "发送完成帧失败", err)
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	response, err = readFrameBytes(reader)
	if err != nil {
		m.failJob(job, "等待远端完成确认失败", err)
		return
	}

	if err := json.Unmarshal(response, &accept); err != nil || accept.Type != "complete" {
		m.failJob(job, "远端未确认落盘完成", decodeFrameError(response, err))
		return
	}

	completedAt := time.Now().UTC()
	job.Status = "completed"
	job.CompletedAt = &completedAt
	job.BytesTransferred = job.BytesTotal
	job.CurrentFile = ""
	job.ReceiveDir = accept.ReceiveDir
	job.Message = "传输完成"
	if strings.TrimSpace(accept.ReceiveDir) != "" {
		job.Message = fmt.Sprintf("传输完成，远端已写入 %s", accept.ReceiveDir)
	}

	m.publishJob("transfer.completed", job)
}

func (m *Manager) prepareFiles(paths []string) ([]fileSpec, []string, int64, error) {
	files := make([]fileSpec, 0, len(paths))
	names := make([]string, 0, len(paths))
	var total int64

	for _, path := range paths {
		cleaned := strings.TrimSpace(path)
		if cleaned == "" {
			return nil, nil, 0, errors.New("file path cannot be empty")
		}

		info, err := os.Stat(cleaned)
		if err != nil {
			return nil, nil, 0, err
		}

		if info.IsDir() {
			return nil, nil, 0, fmt.Errorf("directories are not supported yet: %s", cleaned)
		}

		name := sanitizeFileName(filepath.Base(cleaned))
		if name == "" {
			return nil, nil, 0, fmt.Errorf("invalid file name: %s", cleaned)
		}

		files = append(files, fileSpec{Path: cleaned, Name: name, Size: info.Size()})
		names = append(names, name)
		total += info.Size()
	}

	return files, names, total, nil
}

func (m *Manager) prepareReceiveRoot(senderName string, jobID string) (string, error) {
	folderName := fmt.Sprintf("%s-%s", sanitizeFileName(senderName), shortJobID(jobID))
	root := filepath.Join(m.config.ReceiveDir, folderName)

	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}

	return root, nil
}

func (m *Manager) resolveReceivePath(root string, name string) (string, string, error) {
	base := sanitizeFileName(filepath.Base(strings.TrimSpace(name)))
	if base == "" {
		return "", "", errors.New("invalid file name")
	}

	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)
	if prefix == "" {
		prefix = "file"
	}

	candidate := base
	for index := 1; ; index++ {
		path := filepath.Join(root, candidate)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, candidate, nil
		} else if err != nil {
			return "", "", err
		}

		candidate = fmt.Sprintf("%s (%d)%s", prefix, index, ext)
	}
}

func (m *Manager) publishJob(eventType string, job Job) {
	m.publisher.Broadcast(events.New(eventType, job))
}

func (m *Manager) failJob(job Job, message string, err error) {
	completedAt := time.Now().UTC()
	job.Status = "error"
	job.Message = message
	job.Error = err.Error()
	job.CurrentFile = ""
	job.CompletedAt = &completedAt
	m.publishJob("transfer.failed", job)
}

func (m *Manager) chunkSize() int {
	if m.config.ChunkSize > 0 {
		return m.config.ChunkSize
	}

	return 1 << 20
}

func readFrameBytes(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, errors.New("empty frame")
	}

	return trimmed, nil
}

func writeFrame(writer io.Writer, payload any) error {
	frame, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	frame = append(frame, '\n')
	return writeAll(writer, frame)
}

func copyExact(dst io.Writer, src io.Reader, expected int64, buffer []byte, onProgress func(written int64)) error {
	if expected == 0 {
		if onProgress != nil {
			onProgress(0)
		}
		return nil
	}

	var transferred int64
	for transferred < expected {
		limit := len(buffer)
		remaining := expected - transferred
		if remaining < int64(limit) {
			limit = int(remaining)
		}

		n, err := src.Read(buffer[:limit])
		if n > 0 {
			if err := writeAll(dst, buffer[:n]); err != nil {
				return err
			}

			transferred += int64(n)
			if onProgress != nil {
				onProgress(transferred)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) && transferred == expected {
				break
			}

			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("stream ended early after %d/%d bytes: %w", transferred, expected, err)
			}

			return err
		}

		if n == 0 {
			return io.ErrNoProgress
		}
	}

	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written > 0 {
			data = data[written:]
		}

		if err != nil {
			return err
		}

		if written == 0 {
			return io.ErrShortWrite
		}
	}

	return nil
}

func decodeFrameError(frame []byte, unmarshalErr error) error {
	if unmarshalErr != nil {
		return unmarshalErr
	}

	var remote errorFrame
	if err := json.Unmarshal(frame, &remote); err == nil && remote.Type == "error" {
		return errors.New(remote.Message)
	}

	return fmt.Errorf("unexpected frame: %s", string(frame))
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		"\"", "_",
		"/", "_",
		"\\", "_",
		"|", "_",
		"?", "_",
		"*", "_",
	)

	cleaned := strings.TrimSpace(replacer.Replace(name))
	cleaned = strings.Trim(cleaned, ". ")
	if cleaned == "" {
		return "file"
	}

	return cleaned
}

func shortJobID(jobID string) string {
	trimmed := strings.TrimSpace(jobID)
	if len(trimmed) <= 14 {
		return trimmed
	}

	return trimmed[len(trimmed)-14:]
}