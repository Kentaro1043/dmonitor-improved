package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

func ExtractDeb(debPath, rootfs string) error {
	data, err := os.ReadFile(debPath)
	if err != nil {
		return err
	}
	members, err := parseAr(data)
	if err != nil {
		return err
	}
	for name, content := range members {
		if strings.HasPrefix(name, "data.tar") {
			return extractTar(bytes.NewReader(content), name, rootfs)
		}
	}
	return fmt.Errorf("%s has no data.tar member", debPath)
}

func parseAr(data []byte) (map[string][]byte, error) {
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		return nil, fmt.Errorf("not an ar archive")
	}
	out := map[string][]byte{}
	off := 8
	for off+60 <= len(data) {
		header := data[off : off+60]
		off += 60
		name := strings.TrimSpace(string(header[0:16]))
		name = strings.TrimSuffix(name, "/")
		var size int
		if _, err := fmt.Sscanf(string(header[48:58]), "%d", &size); err != nil {
			return nil, fmt.Errorf("parse ar member %q size: %w", name, err)
		}
		if off+size > len(data) {
			return nil, fmt.Errorf("ar member %q exceeds archive size", name)
		}
		out[name] = append([]byte(nil), data[off:off+size]...)
		off += size
		if off%2 == 1 {
			off++
		}
	}
	return out, nil
}

func extractTar(r io.Reader, name, rootfs string) error {
	var tr *tar.Reader
	switch {
	case strings.HasSuffix(name, ".gz"):
		gz, err := gzip.NewReader(r)
		if err != nil {
			return err
		}
		defer gz.Close()
		tr = tar.NewReader(gz)
	case strings.HasSuffix(name, ".xz"):
		xzr, err := xz.NewReader(r)
		if err != nil {
			return err
		}
		tr = tar.NewReader(xzr)
	default:
		tr = tar.NewReader(r)
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(rootfs, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(h.Mode)); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			linkTarget, err := safeJoin(rootfs, h.Linkname)
			if err != nil {
				return err
			}
			if err := os.Link(linkTarget, target); err != nil {
				return err
			}
		}
	}
}

func safeJoin(rootfs, name string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." {
		return rootfs, nil
	}
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return filepath.Join(rootfs, clean), nil
}
