package sqliteruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// DefaultMaxCopyBytes caps a SQLite database and its WAL copied for read-only inspection.
const DefaultMaxCopyBytes = 512 << 20

var errChanged = errors.New("sqliteruntime: database changed while copying")

// FileState captures the size and modification times of a SQLite database and
// its WAL; WALSize is -1 when no WAL file is present.
type FileState struct {
	DBSize, WALSize   int64
	DBMtime, WALMtime time.Time
}

// Same reports whether two FileState values represent the same underlying files.
func (s FileState) Same(o FileState) bool {
	return s.DBSize == o.DBSize && s.WALSize == o.WALSize &&
		s.DBMtime.Equal(o.DBMtime) && s.WALMtime.Equal(o.WALMtime)
}

// StateOf inspects the SQLite database at path and its optional -wal sidecar.
func StateOf(path string) (FileState, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileState{}, err
	}
	if !info.Mode().IsRegular() {
		return FileState{}, fmt.Errorf("sqliteruntime: %s is not a regular file", filepath.Base(path))
	}
	state := FileState{DBSize: info.Size(), DBMtime: info.ModTime(), WALSize: -1}
	wal, err := os.Lstat(path + "-wal")
	switch {
	case err == nil && wal.Mode().IsRegular():
		state.WALSize, state.WALMtime = wal.Size(), wal.ModTime()
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return FileState{}, err
	}
	return state, nil
}

// WithCopy copies the SQLite database at path and its -wal sidecar into a
// private temporary directory, opens the copy read-only via SQLite, and passes
// it to read. Live files owned by external CLIs are never opened or locked.
func WithCopy(ctx context.Context, path string, maxBytes int64, read func(db *sqlite.DB, copyPath string) error) (FileState, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCopyBytes
	}
	if err := Load(); err != nil {
		return FileState{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return FileState{}, err
		}
		before, err := StateOf(path)
		if err != nil {
			return FileState{}, err
		}
		if before.DBSize+max(0, before.WALSize) > maxBytes {
			return FileState{}, fmt.Errorf("sqliteruntime: %s is too large", filepath.Base(path))
		}
		dir, err := os.MkdirTemp("", "quosparr-sqlite-")
		if err != nil {
			return FileState{}, err
		}
		state, err := copyAndRead(ctx, path, dir, maxBytes, before, read)
		_ = os.RemoveAll(dir)
		if errors.Is(err, errChanged) {
			continue
		}
		return state, err
	}
	return FileState{}, errChanged
}

func copyAndRead(ctx context.Context, path, dir string, maxBytes int64, before FileState, read func(db *sqlite.DB, copyPath string) error) (FileState, error) {
	copyPath := filepath.Join(dir, "copy.db")
	if err := copyFile(path, copyPath, maxBytes); err != nil {
		return FileState{}, err
	}
	if before.WALSize >= 0 {
		if err := copyFile(path+"-wal", copyPath+"-wal", maxBytes); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return FileState{}, err
		}
	}
	after, err := StateOf(path)
	if err != nil {
		return FileState{}, err
	}
	if !after.Same(before) {
		return FileState{}, errChanged
	}
	db, err := sqlite.Open(ctx, copyPath, sqlite.OpenOptions{BusyTimeout: time.Second})
	if err != nil {
		return FileState{}, err
	}
	err = read(db, copyPath)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	return after, err
}

func copyFile(from, to string, maxBytes int64) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("sqliteruntime: %s is not a regular file", filepath.Base(from))
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, io.LimitReader(src, maxBytes+1))
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	return err
}
