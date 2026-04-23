package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFileCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.bin")
	if err := AtomicWriteFile(path, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	// No leftover tmp sibling after success.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp sibling should be gone: %v", err)
	}
}

func TestAtomicWriteFileOverwritesExisting(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.bin")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
}

func TestAtomicWriteFileNoTmpOnParentMissing(t *testing.T) {
	// If the parent directory doesn't exist, the tmp open fails and no file
	// is left behind.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing-dir", "x.bin")
	err := AtomicWriteFile(path, []byte("x"), 0o640)
	if err == nil {
		t.Fatal("expected error for missing parent dir")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp sibling should not exist on failure")
	}
}
