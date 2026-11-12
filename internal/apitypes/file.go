// Package apitypes holds small DTOs shared between the Incus client wrapper
// and the services without creating import cycles in tests.
package apitypes

import "time"

// FileEntry describes one file or directory inside an instance.
type FileEntry struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"modtime"`
}
