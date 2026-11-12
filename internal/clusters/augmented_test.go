package clusters

import (
	"os"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

type fakeFileInfo struct {
	name  string
	size  int64
	dir   bool
	mode  os.FileMode
	mtime time.Time
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return f.mtime }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

func TestFileEntriesOrdersDirectoriesFirst(t *testing.T) {
	mtime := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
	entries := fileEntries([]os.FileInfo{
		fakeFileInfo{name: "z.log", size: 10, mtime: mtime},
		fakeFileInfo{name: "data", dir: true, mode: 0o755, mtime: mtime},
		fakeFileInfo{name: "a.log", size: 5, mode: 0o644, mtime: mtime},
	})

	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	if !entries[0].IsDir || entries[0].Name != "data" {
		t.Fatalf("entries[0] = %#v, want directory first", entries[0])
	}
	if entries[1].Name != "a.log" || entries[2].Name != "z.log" {
		t.Fatalf("files not sorted: %#v", entries[1:])
	}
	if entries[1].Mode != uint32(0o644) || entries[1].Size != 5 {
		t.Fatalf("entry detail = %#v", entries[1])
	}

	if empty := fileEntries(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("empty listing = %#v", empty)
	}
}

func TestFullClientUnwrapsAugmented(t *testing.T) {
	// The wrapper itself satisfies the raw SDK interface via embedding, so a
	// type assertion exposes the embedded client (nil here, which proves the
	// unwrap happened).
	wrapped := &augmentedClient{}
	if got := fullClient(wrapped); got != nil {
		t.Fatalf("fullClient() = %v, want the embedded client", got)
	}
	// The fake embeds the full SDK interface, so it doubles as a raw client.
	if fullClient(&incusfake.Server{}) == nil {
		t.Fatal("fullClient() lost the raw fake")
	}
}
