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
	if err := os.MkdirAll(attachmentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir %s: %w", attachmentDir, err)
	}
	return &Local{attachmentDir: attachmentDir}, nil
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

func (l *Local) attachmentPath(key string) (string, error) {
	return safeJoin(l.attachmentDir, key)
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
