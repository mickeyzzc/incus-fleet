package remotewrite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mickeyzzc/incus-fleet/internal/telemetry"
)

const (
	// DefaultMaxBytes bounds the on-disk spool (64 MiB).
	DefaultMaxBytes int64 = 64 << 20
	spoolSuffix           = ".json"
)

type spoolEntry struct {
	path   string
	series []telemetry.Series
	size   int64
}

// spool is an append-only directory of JSON batches. Filenames are sortable
// timestamps; the oldest files are removed first when retention is exceeded.
type spool struct {
	root       string
	partition  string
	dir        string
	maxEntries int
	maxBytes   int64
	sequence   atomic.Uint64
	lock       *os.File
	closeOnce  sync.Once

	mu        sync.Mutex
	entries   int
	sizeBytes int64
}

const spoolLockName = "lock"

func openSpool(root, partition string, maxEntries int, maxBytes int64) (*spool, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("remotewrite: spool directory is required")
	}
	partition = sanitizePartition(partition)
	if maxEntries <= 0 {
		maxEntries = 10000
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("remotewrite: create spool root: %w", err)
	}

	// Serialize startup claims across instances that share one spool root.
	rootLock, err := lockRootFile(filepath.Join(root, ".lock"))
	if err != nil {
		return nil, fmt.Errorf("remotewrite: lock spool root: %w", err)
	}
	defer unlockFile(rootLock)

	dir := filepath.Join(root, partition)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("remotewrite: create spool partition: %w", err)
	}
	ownLock, err := lockFile(filepath.Join(dir, spoolLockName))
	if err != nil {
		return nil, fmt.Errorf("remotewrite: partition %q is already owned: %w", partition, err)
	}

	if err := migrateLegacyRoot(root, dir); err != nil {
		unlockFile(ownLock)
		return nil, err
	}
	if err := claimStalePartitions(root, dir); err != nil {
		unlockFile(ownLock)
		return nil, err
	}

	return &spool{
		root: root, partition: partition, dir: dir,
		maxEntries: maxEntries, maxBytes: maxBytes,
		lock: ownLock,
	}, nil
}

// Close releases the partition lock. Writers must call it after their sender
// goroutine has stopped.
func (s *spool) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.lock != nil {
			unlockFile(s.lock)
			s.lock = nil
		}
	})
}

func lockFile(path string) (*os.File, error) {
	return flockFile(path, unix.LOCK_EX|unix.LOCK_NB)
}

func lockRootFile(path string) (*os.File, error) {
	return flockFile(path, unix.LOCK_EX)
}

func flockFile(path string, how int) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // spool path is operator-configured
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), how); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func unlockFile(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

// claimStalePartitions moves batches from unlocked partitions into the current
// partition. Root startup locking ensures two new owners cannot claim at once.
func claimStalePartitions(root, ownDir string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("remotewrite: read spool root: %w", err)
	}
	seq := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(root, entry.Name())
		if source == ownDir {
			continue
		}
		lock, err := lockFile(filepath.Join(source, spoolLockName))
		if err != nil {
			// Locked by a live instance.
			continue
		}
		files, readErr := os.ReadDir(source)
		if readErr == nil {
			for _, file := range files {
				if file.IsDir() || !strings.HasSuffix(file.Name(), spoolSuffix) {
					continue
				}
				seq++
				target := filepath.Join(
					ownDir,
					fmt.Sprintf("taken-%020d-%06d%s", time.Now().UTC().UnixNano(), seq, spoolSuffix),
				)
				if err := os.Rename(filepath.Join(source, file.Name()), target); err != nil {
					unlockFile(lock)
					return fmt.Errorf("remotewrite: claim stale spool batch: %w", err)
				}
			}
		}
		unlockFile(lock)
		_ = os.Remove(filepath.Join(source, spoolLockName))
		_ = os.Remove(source)
	}
	return nil
}

func sanitizePartition(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" || out == "." || out == ".." {
		if strings.TrimSpace(value) == "" {
			return defaultPartition()
		}
		return "default"
	}
	return out
}

func defaultPartition() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "center"
	}
	return fmt.Sprintf("%s-%d", sanitizePartitionName(hostname), os.Getpid())
}

func sanitizePartitionName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "center"
	}
	return out
}

// migrateLegacyRoot moves batches written by the pre-partition layout into the
// caller's partition so upgrades keep replaying old data.
func migrateLegacyRoot(root, partitionDir string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("remotewrite: read spool root: %w", err)
	}
	seq := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), spoolSuffix) {
			continue
		}
		seq++
		target := filepath.Join(
			partitionDir,
			fmt.Sprintf("legacy-%020d-%06d%s", time.Now().UTC().UnixNano(), seq, spoolSuffix),
		)
		if err := os.Rename(filepath.Join(root, entry.Name()), target); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("remotewrite: migrate legacy spool batch: %w", err)
		}
	}
	return nil
}

func (s *spool) append(series []telemetry.Series) (spoolEntry, error) {
	if len(series) == 0 {
		return spoolEntry{}, fmt.Errorf("remotewrite: empty spool batch")
	}
	data, err := json.Marshal(series)
	if err != nil {
		return spoolEntry{}, fmt.Errorf("remotewrite: encode spool batch: %w", err)
	}
	sequence := s.sequence.Add(1)
	name := fmt.Sprintf("%020d-%06d%s", time.Now().UTC().UnixNano(), sequence%1_000_000, spoolSuffix)
	final := filepath.Join(s.dir, name)
	tmp := final + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // spool directory is operator-configured
	if err != nil {
		return spoolEntry{}, fmt.Errorf("remotewrite: open spool batch: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return spoolEntry{}, fmt.Errorf("remotewrite: write spool batch: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return spoolEntry{}, fmt.Errorf("remotewrite: sync spool batch: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return spoolEntry{}, fmt.Errorf("remotewrite: close spool batch: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return spoolEntry{}, fmt.Errorf("remotewrite: install spool batch: %w", err)
	}
	if dir, err := os.Open(s.dir); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	entry := spoolEntry{path: final, series: series, size: int64(len(data))}
	s.mu.Lock()
	s.entries++
	s.sizeBytes += entry.size
	over := s.entries > s.maxEntries || s.sizeBytes > s.maxBytes
	s.mu.Unlock()
	if over {
		s.trim()
	}

	return entry, nil
}

// load returns spool entries oldest first after applying retention. Corrupt
// files are removed so they cannot block replay forever.
func (s *spool) load() ([]spoolEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("remotewrite: read spool: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), spoolSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var (
		totalBytes int64
		keep       = make([]string, 0, len(names))
	)
	for _, name := range names {
		path := filepath.Join(s.dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		keep = append(keep, name)
		totalBytes += info.Size()
	}
	for len(keep) > s.maxEntries || (totalBytes > s.maxBytes && len(keep) > 0) {
		oldest := filepath.Join(s.dir, keep[0])
		if info, err := os.Stat(oldest); err == nil {
			totalBytes -= info.Size()
		}
		_ = os.Remove(oldest)
		keep = keep[1:]
	}

	out := make([]spoolEntry, 0, len(keep))
	var loadedBytes int64
	for _, name := range keep {
		path := filepath.Join(s.dir, name)
		data, err := os.ReadFile(path) //nolint:gosec // spool directory owned by the service
		if err != nil {
			continue
		}
		var series []telemetry.Series
		if err := json.Unmarshal(data, &series); err != nil || len(series) == 0 {
			_ = os.Remove(path)
			continue
		}
		entry := spoolEntry{path: path, series: series, size: int64(len(data))}
		out = append(out, entry)
		loadedBytes += entry.size
	}
	s.mu.Lock()
	s.entries = len(out)
	s.sizeBytes = loadedBytes
	s.mu.Unlock()

	return out, nil
}

func (s *spool) remove(entry spoolEntry) {
	if entry.path == "" {
		return
	}
	if err := os.Remove(entry.path); err == nil {
		s.mu.Lock()
		s.entries--
		s.sizeBytes -= entry.size
		s.mu.Unlock()
	}
}

// trim removes the oldest files until both retention limits are satisfied.
func (s *spool) trim() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries <= s.maxEntries && s.sizeBytes <= s.maxBytes {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), spoolSuffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	total := int64(0)
	for _, name := range names {
		if info, err := entriesInfo(s.dir, name); err == nil {
			total += info
		}
	}
	count := len(names)
	for (count > s.maxEntries || total > s.maxBytes) && len(names) > 0 {
		oldest := filepath.Join(s.dir, names[0])
		if info, err := entriesInfo(s.dir, names[0]); err == nil {
			total -= info
		}
		_ = os.Remove(oldest)
		names = names[1:]
		count--
	}
	s.entries = count
	s.sizeBytes = total
}

func entriesInfo(dir, name string) (int64, error) {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// count returns the number of spool files, used by tests and diagnostics.
func (s *spool) count() int {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), spoolSuffix) {
			count++
		}
	}
	return count
}

// bytes returns the total spool size in bytes.
func (s *spool) bytes() int64 {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), spoolSuffix) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}
