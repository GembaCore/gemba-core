package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPlainVerifier_ConstantTimeCompare(t *testing.T) {
	v := NewPlainVerifier("s3cret")
	if !v.Verify("s3cret") {
		t.Fatal("valid token rejected")
	}
	if v.Verify("s3cret ") || v.Verify("") || v.Verify("other") {
		t.Fatal("invalid token accepted")
	}
}

func TestHashVerifier_RotateWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")

	// First token + hash on disk.
	oldHash, err := HashToken("old-token")
	if err != nil {
		t.Fatalf("HashToken: %v", err)
	}
	if err := WriteHash(path, oldHash); err != nil {
		t.Fatalf("WriteHash: %v", err)
	}

	v := NewHashVerifier(path)
	if !v.Verify("old-token") {
		t.Fatal("old token rejected before rotation")
	}
	if v.Verify("new-token") {
		t.Fatal("new token accepted before rotation")
	}

	// Bump mtime: some filesystems have 1s resolution so step past it so
	// stat() sees a different ModTime.
	time.Sleep(1100 * time.Millisecond)

	// Rotate on disk (simulates `gemba auth token rotate` in another
	// process while the server is still running).
	newHash, err := HashToken("new-token")
	if err != nil {
		t.Fatalf("HashToken new: %v", err)
	}
	if err := WriteHash(path, newHash); err != nil {
		t.Fatalf("WriteHash new: %v", err)
	}

	if v.Verify("old-token") {
		t.Fatal("old token still accepted after rotation")
	}
	if !v.Verify("new-token") {
		t.Fatal("new token rejected after rotation")
	}
}

func TestHashVerifier_MissingFile_RejectsEverything(t *testing.T) {
	v := NewHashVerifier(filepath.Join(t.TempDir(), "does-not-exist"))
	if v.Verify("anything") {
		t.Fatal("missing file should reject all tokens")
	}
}

func TestHashVerifier_FileDeletedAfterLoad_RejectsSubsequent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "primary")
	hash, _ := HashToken("tok")
	if err := WriteHash(path, hash); err != nil {
		t.Fatalf("WriteHash: %v", err)
	}
	v := NewHashVerifier(path)
	if !v.Verify("tok") {
		t.Fatal("token rejected before deletion")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if v.Verify("tok") {
		t.Fatal("token still accepted after file removal")
	}
}
