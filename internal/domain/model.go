package domain

import "time"

// Metadata describes a running codetap instance.
type Metadata struct {
	Name      string    `json:"name"`
	Commit    string    `json:"commit"`
	Arch      string    `json:"arch"`
	Folder    string    `json:"folder"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// SocketEntry is a discovered socket with its metadata and liveness state.
type SocketEntry struct {
	Name     string
	Path     string
	Metadata Metadata
	Alive    bool
}
