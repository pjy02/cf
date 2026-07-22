package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyChecksumAndExtractBinary(t *testing.T) {
	dir := t.TempDir()
	archiveName := "cf_1.2.0_linux_amd64.tar.gz"
	archivePath := filepath.Join(dir, archiveName)
	binary := []byte("fake executable")
	if err := os.WriteFile(archivePath, makeArchive(t, "cf", binary), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(mustRead(t, archivePath))
	digest := hex.EncodeToString(hash[:])
	checksums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksums, []byte(digest+"  "+archiveName+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, err := verifyChecksum(archivePath, checksums, archiveName)
	if err != nil || actual != digest {
		t.Fatalf("checksum failed: %s %v", actual, err)
	}
	destination := filepath.Join(dir, "cf.new")
	if err := extractBinary(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, destination); !bytes.Equal(got, binary) {
		t.Fatalf("wrong extracted binary: %q", got)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "cf.tar.gz")
	checksums := filepath.Join(dir, "checksums.txt")
	_ = os.WriteFile(archive, []byte("bad"), 0o600)
	_ = os.WriteFile(checksums, []byte(strings.Repeat("0", 64)+"  cf.tar.gz\n"), 0o600)
	if _, err := verifyChecksum(archive, checksums, "cf.tar.gz"); err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

func TestExtractBinaryRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.tar.gz")
	if err := os.WriteFile(archive, makeArchive(t, "../cf", []byte("bad")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, filepath.Join(dir, "cf.new")); err == nil {
		t.Fatal("unsafe archive path was accepted")
	}
}

func makeArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
