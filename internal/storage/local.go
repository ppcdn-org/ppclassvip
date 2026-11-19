package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ppclassvip/internal/config"
)

var (
	ErrFileTooLarge = errors.New("file too large")
	ErrInvalidKey   = errors.New("invalid file key")
)

type StoredFile struct {
	Key      string
	Size     int64
	SHA256   string
	FullPath string
}

type Local struct {
	attachmentDir string
	recordingDir  string
}

func NewLocal(cfg config.StorageConfig) (*Local, error) {
	dataDir := strings.TrimSpace(cfg.DataDir)
	if dataDir == "" {
		dataDir = "./data"
	}
	attachmentDir := strings.TrimSpace(cfg.AttachmentDir)
	if attachmentDir == "" {
		attachmentDir = filepath.Join(dataDir, "attachments")
	}
	recordingDir := strings.TrimSpace(cfg.RecordingDir)
	if recordingDir == "" {
		recordingDir = filepath.Join(dataDir, "recordings")
	}
	for _, dir := range []string{
		attachmentDir,
		recordingDir,
		filepath.Join(recordingDir, "tmp"),
		filepath.Join(recordingDir, "final"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create storage dir %s: %w", dir, err)
		}
	}
	return &Local{attachmentDir: attachmentDir, recordingDir: recordingDir}, nil
}

func (l *Local) SaveAttachment(roomID uint64, src io.Reader, maxBytes int64) (StoredFile, error) {
	if maxBytes <= 0 {
		return StoredFile{}, ErrFileTooLarge
	}
	random, err := randomHex(16)
	if err != nil {
		return StoredFile{}, err
	}
	key := filepath.ToSlash(filepath.Join(strconv.FormatUint(roomID, 10), time.Now().UTC().Format("200601"), random))
	fullPath, err := l.attachmentPath(key)
	if err != nil {
		return StoredFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return StoredFile{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(fullPath), ".upload-*")
	if err != nil {
		return StoredFile{}, err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	hasher := sha256.New()
	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	written, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		return StoredFile{}, err
	}
	if written > maxBytes {
		return StoredFile{}, ErrFileTooLarge
	}
	if err := tmp.Sync(); err != nil {
		return StoredFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return StoredFile{}, err
	}
	if err := os.Rename(tmpName, fullPath); err != nil {
		return StoredFile{}, err
	}
	return StoredFile{
		Key:      key,
		Size:     written,
		SHA256:   hex.EncodeToString(hasher.Sum(nil)),
		FullPath: fullPath,
	}, nil
}

func (l *Local) OpenAttachment(key string) (*os.File, error) {
	fullPath, err := l.attachmentPath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (l *Local) DeleteAttachment(key string) error {
	fullPath, err := l.attachmentPath(key)
	if err != nil {
		return err
	}
	return removeIfExists(fullPath)
}

func (l *Local) AppendRecordingChunk(recordingID string, src io.Reader, maxBytes int64) (int64, error) {
	if err := validateID(recordingID); err != nil {
		return 0, err
	}
	tmpPath := l.recordingTempPath(recordingID)
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	limited := &io.LimitedReader{R: src, N: maxBytes + 1}
	written, err := io.Copy(file, limited)
	if err != nil {
		return 0, err
	}
	if written > maxBytes {
		return 0, ErrFileTooLarge
	}
	return written, nil
}

func (l *Local) RecordingTempSize(recordingID string) (int64, error) {
	if err := validateID(recordingID); err != nil {
		return 0, err
	}
	info, err := os.Stat(l.recordingTempPath(recordingID))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func (l *Local) FinalizeRecording(roomID uint64, recordingID string) (StoredFile, error) {
	if err := validateID(recordingID); err != nil {
		return StoredFile{}, err
	}
	key := filepath.ToSlash(filepath.Join("final", strconv.FormatUint(roomID, 10), recordingID+".webm"))
	fullPath, err := l.recordingPath(key)
	if err != nil {
		return StoredFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return StoredFile{}, err
	}
	tmpPath := l.recordingTempPath(recordingID)
	info, err := os.Stat(tmpPath)
	if err != nil {
		if os.IsNotExist(err) {
			if finalInfo, statErr := os.Stat(fullPath); statErr == nil {
				return StoredFile{Key: key, Size: finalInfo.Size(), FullPath: fullPath}, nil
			}
		}
		return StoredFile{}, err
	}
	_ = os.Remove(fullPath)
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return StoredFile{}, err
	}
	return StoredFile{Key: key, Size: info.Size(), FullPath: fullPath}, nil
}

func (l *Local) OpenRecording(key string) (*os.File, error) {
	fullPath, err := l.recordingPath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (l *Local) DeleteRecording(roomID uint64, recordingID, key string) error {
	var firstErr error
	if err := validateID(recordingID); err == nil {
		if err := removeIfExists(l.recordingTempPath(recordingID)); err != nil {
			firstErr = err
		}
	}
	if strings.TrimSpace(key) == "" {
		key = filepath.ToSlash(filepath.Join("final", strconv.FormatUint(roomID, 10), recordingID+".webm"))
	}
	if fullPath, err := l.recordingPath(key); err == nil {
		if err := removeIfExists(fullPath); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *Local) attachmentPath(key string) (string, error) {
	return safeJoin(l.attachmentDir, key)
}

func (l *Local) recordingPath(key string) (string, error) {
	return safeJoin(l.recordingDir, key)
}

func (l *Local) recordingTempPath(recordingID string) string {
	return filepath.Join(l.recordingDir, "tmp", recordingID+".part")
}

func safeJoin(base, key string) (string, error) {
	key = filepath.ToSlash(strings.TrimSpace(key))
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return "", ErrInvalidKey
	}
	full := filepath.Join(base, filepath.FromSlash(key))
	rel, err := filepath.Rel(base, full)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", ErrInvalidKey
	}
	return full, nil
}

func validateID(id string) error {
	if id == "" || len(id) > 40 {
		return ErrInvalidKey
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return ErrInvalidKey
		}
	}
	return nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
