package installer

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyNames(t *testing.T) {
	got := dependencyNames("libc6 (>= 2.34), libssl3 | libssl1.1, zlib1g")
	want := []string{"libc6", "libssl3", "zlib1g"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("got[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}

func TestExtractTarOverwritesReadOnlyFile(t *testing.T) {
	root := t.TempDir()
	archive := tarWithFile(t, "./etc/sudoers.d/010_www-data-nopasswd", 0o440, "first\n")
	if err := extractTar(bytes.NewReader(archive), "data.tar", root); err != nil {
		t.Fatal(err)
	}
	archive = tarWithFile(t, "./etc/sudoers.d/010_www-data-nopasswd", 0o440, "second\n")
	if err := extractTar(bytes.NewReader(archive), "data.tar", root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "etc", "sudoers.d", "010_www-data-nopasswd")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second\n" {
		t.Fatalf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o440 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func tarWithFile(t *testing.T, name string, mode int64, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "./", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: filepath.Dir(name), Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
