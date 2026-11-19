package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"ppclassvip/internal/config"
)

func testStorage(t *testing.T) *Local {
	t.Helper()
	local, err := NewLocal(config.StorageConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return local
}

func TestSaveAndOpenAttachment(t *testing.T) {
	local := testStorage(t)
	stored, err := local.SaveAttachment(9, strings.NewReader("hello"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("hello"))
	if stored.Size != 5 || stored.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("stored = %#v", stored)
	}
	file, err := local.OpenAttachment(stored.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "hello" {
		t.Fatalf("data = %q err = %v", data, err)
	}
}

func TestSaveAttachmentRejectsOversize(t *testing.T) {
	local := testStorage(t)
	if _, err := local.SaveAttachment(9, strings.NewReader("too large"), 3); err != ErrFileTooLarge {
		t.Fatalf("err = %v", err)
	}
}

func TestAttachmentPathTraversalRejected(t *testing.T) {
	local := testStorage(t)
	if _, err := local.OpenAttachment("../secret"); err != ErrInvalidKey {
		t.Fatalf("err = %v", err)
	}
}
