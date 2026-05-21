package transfer

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/landrop/landrop/backend/internal/events"
)

type Request struct {
	TargetID string   `json:"targetId"`
	Files    []string `json:"files"`
}

type Job struct {
	ID        string    `json:"id"`
	TargetID  string    `json:"targetId"`
	Files     []string  `json:"files"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type Manager struct {
	publisher events.Publisher
}

func NewManager(publisher events.Publisher) *Manager {
	return &Manager{publisher: publisher}
}

func (m *Manager) Queue(request Request) (Job, error) {
	if strings.TrimSpace(request.TargetID) == "" {
		return Job{}, errors.New("targetId is required")
	}

	if len(request.Files) == 0 {
		return Job{}, errors.New("at least one file is required")
	}

	job := Job{
		ID:        fmt.Sprintf("job-%d", time.Now().UnixNano()),
		TargetID:  request.TargetID,
		Files:     request.Files,
		Status:    "queued",
		Message:   "传输任务已排队，等待真实文件流式发送器接管。",
		CreatedAt: time.Now().UTC(),
	}

	m.publisher.Broadcast(events.New("transfer.queued", job))

	return job, nil
}