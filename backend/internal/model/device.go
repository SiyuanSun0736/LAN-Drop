package model

import "time"

type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	OS       string    `json:"os"`
	Address  string    `json:"address"`
	Port     int       `json:"port"`
	Protocol string    `json:"protocol"`
	Version  string    `json:"version"`
	LastSeen time.Time `json:"lastSeen"`
}