package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplicationLogRotatesAtLimitAndKeepsOnlyLatestBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	logFile, err := openApplicationLog(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	for _, payload := range [][]byte{[]byte("12345678"), []byte("A"), []byte("BCDEFG"), []byte("!!")} {
		if written, err := logFile.Write(payload); err != nil || written != len(payload) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", payload, written, err, len(payload))
		}
	}

	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "!!" {
		t.Fatalf("active log = %q, want !!", active)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "ABCDEFG" {
		t.Fatalf("rotated log = %q, want ABCDEFG", backup)
	}
}

func TestApplicationLogRotationFailurePreservesExistingBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	logFile, err := openApplicationLog(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()

	if _, err := logFile.Write([]byte("A")); err != nil {
		t.Fatal(err)
	}
	backupPath := path + ".1"
	if err := os.Mkdir(backupPath, 0o700); err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(backupPath, "prior-backup")
	if err := os.WriteFile(markerPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if written, err := logFile.Write([]byte("B")); err == nil || written != 0 {
		t.Fatalf("Write with an unreplaceable backup = %d, %v; want 0 and an error", written, err)
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("prior backup marker was lost: %v", err)
	}
	if string(marker) != "keep" {
		t.Fatalf("prior backup marker = %q, want keep", marker)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "A" {
		t.Fatalf("active log = %q after failed rotation, want A", active)
	}

	if err := os.RemoveAll(backupPath); err != nil {
		t.Fatal(err)
	}
	if written, err := logFile.Write([]byte("B")); err != nil || written != 1 {
		t.Fatalf("Write after the rotation failure = %d, %v; want 1, nil", written, err)
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "A" {
		t.Fatalf("recovered rotated log = %q, want A", backup)
	}
}
