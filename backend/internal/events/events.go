package events

import "time"

type Event struct {
	Type    string    `json:"type"`
	Payload any       `json:"payload,omitempty"`
	Time    time.Time `json:"time"`
}

type Publisher interface {
	Broadcast(event Event)
}

func New(eventType string, payload any) Event {
	return Event{
		Type:    eventType,
		Payload: payload,
		Time:    time.Now().UTC(),
	}
}