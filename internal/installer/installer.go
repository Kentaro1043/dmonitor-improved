package installer

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ulikunitz/xz"
)

const (
	jarlRepo           = "http://app.d-star.info/debian/bookworm/"
	jarlKeyURL         = "http://app.d-star.info/debian/bookworm/jarl-pkg.key"
	jarlKeyFingerprint = "6C3FEFE83D174831AF4776670AE816B1B6093B5A"
	dmonitorPackage    = "dmonitor"
	dmonitorVersion    = "02.00"
	dmonitorSHA256     = "ebb8085186214a337943129ed6ec3d8e6e29a1856a50add6e88691568d2c54ea"
)

var bootstrapDeps = []string{
	"libc6",
	"libssl3",
	"libusb-0.1-4",
	"libgcc-s1",
	"zlib1g",
}

type Options struct {
	RootFS            string
	CacheDir          string
	InstallDeps       bool
	VerifyFingerprint bool
}

type Installer struct {
	opts   Options
	client *http.Client
}

func New(opts Options) *Installer {
	if opts.RootFS == "" {
		opts.RootFS = filepath.Join("runtime", "rootfs")
	}
	if opts.CacheDir == "" {
		opts.CacheDir = filepath.Join("runtime", "cache")
	}
	return &Installer{opts: opts, client: http.DefaultClient}
}

func (i *Installer) Run(ctx context.Context) error {
	if err := os.MkdirAll(i.opts.RootFS, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(i.opts.CacheDir, 0o755); err != nil {
		return err
	}
	if i.opts.VerifyFingerprint {
		if err := i.verifyKey(ctx); err != nil {
			return err
		}
	}

	jarlIndex, err := i.fetchFlatPackages(ctx, jarlRepo)
	if err != nil {
		return err
	}
	dmonitorPkg, ok := findPackage(jarlIndex, dmonitorPackage, "armhf")
	if !ok {
		return errors.New("dmonitor armhf package was not found in the JARL repository")
	}
	if !strings.HasPrefix(dmonitorPkg.Version, dmonitorVersion) {
		return fmt.Errorf("unexpected dmonitor version %q", dmonitorPkg.Version)
	}
	deb, err := i.downloadPackage(ctx, jarlRepo, dmonitorPkg)
	if err != nil {
		return err
	}
	if err := verifyFileSHA256(deb, dmonitorSHA256); err != nil {
		return err
	}
	if err := ExtractDeb(deb, i.opts.RootFS); err != nil {
		return fmt.Errorf("extract dmonitor: %w", err)
	}

	if i.opts.InstallDeps {
		if err := i.installDependencies(ctx, jarlIndex); err != nil {
			return err
		}
	}
	return i.ensureLayout()
}

func (i *Installer) verifyKey(ctx context.Context) error {
	keyPath := filepath.Join(i.opts.CacheDir, "jarl-pkg.key")
	if err := i.downloadFile(ctx, jarlKeyURL, keyPath); err != nil {
		return err
	}
	gpg, err := exec.LookPath("gpg")
	if err != nil {
		return fmt.Errorf("gpg is required to verify the JARL key fingerprint: %w", err)
	}
	out, err := exec.CommandContext(ctx, gpg, "--show-keys", "--with-colons", keyPath).Output()
	if err != nil {
		return fmt.Errorf("verify key fingerprint: %w", err)
	}
	if !bytes.Contains(out, []byte("fpr:::::::::"+jarlKeyFingerprint+":")) {
		return fmt.Errorf("unexpected JARL key fingerprint; expected %s", jarlKeyFingerprint)
	}
	return nil
}

func (i *Installer) installDependencies(ctx context.Context, jarlIndex []Package) error {
	if wiringpi, ok := findPackage(jarlIndex, "wiringpi", "armhf"); ok {
		deb, err := i.downloadPackage(ctx, jarlRepo, wiringpi)
		if err != nil {
			return err
		}
		if wiringpi.SHA256 != "" {
			if err := verifyFileSHA256(deb, wiringpi.SHA256); err != nil {
				return err
			}
		}
		if err := ExtractDeb(deb, i.opts.RootFS); err != nil {
			return fmt.Errorf("extract wiringpi: %w", err)
		}
	}

	debian, err := i.fetchDebianPackages(ctx)
	if err != nil {
		return err
	}
	queue := append([]string{}, bootstrapDeps...)
	seen := map[string]bool{dmonitorPackage: true}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		pkg, ok := findPackage(debian, name, "armhf")
		if !ok {
			continue
		}
		deb, err := i.downloadPackage(ctx, debianRepoBase(), pkg)
		if err != nil {
			return err
		}
		if pkg.SHA256 != "" {
			if err := verifyFileSHA256(deb, pkg.SHA256); err != nil {
				return err
			}
		}
		if err := ExtractDeb(deb, i.opts.RootFS); err != nil {
			return fmt.Errorf("extract %s: %w", pkg.Package, err)
		}
		queue = append(queue, dependencyNames(pkg.Depends)...)
	}
	return nil
}

func (i *Installer) ensureLayout() error {
	for _, dir := range []string{
		"var/www",
		"var/tmp",
		"var/log",
		"var/run",
		"dev",
		"usr/lib",
	} {
		if err := os.MkdirAll(filepath.Join(i.opts.RootFS, dir), 0o755); err != nil {
			return err
		}
	}
	defaults := map[string]string{
		"var/www/dmonitor.conf": "ICOM\nNONE\n        \nNO_SKIP\n",
		"var/www/buff_hold.txt": "0\n",
		"var/www/rpt_mast.txt":  "",
	}
	for rel, content := range defaults {
		p := filepath.Join(i.opts.RootFS, rel)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) fetchFlatPackages(ctx context.Context, base string) ([]Package, error) {
	p := filepath.Join(i.opts.CacheDir, "jarl-Packages")
	if err := i.downloadFile(ctx, base+"Packages", p); err != nil {
		if err := i.downloadFile(ctx, base+"Packages.gz", p+".gz"); err != nil {
			return nil, err
		}
		f, err := os.Open(p + ".gz")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		out, err := os.Create(p)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, gz); err != nil {
			_ = out.Close()
			return nil, err
		}
		if err := out.Close(); err != nil {
			return nil, err
		}
	}
	return parsePackagesFile(p)
}

func (i *Installer) fetchDebianPackages(ctx context.Context) ([]Package, error) {
	url := debianRepoBase() + "/dists/bookworm/main/binary-armhf/Packages.xz"
	cache := filepath.Join(i.opts.CacheDir, "debian-bookworm-main-armhf-Packages")
	if err := i.downloadFile(ctx, url, cache+".xz"); err != nil {
		return nil, err
	}
	in, err := os.Open(cache + ".xz")
	if err != nil {
		return nil, err
	}
	defer in.Close()
	xzr, err := xz.NewReader(in)
	if err != nil {
		return nil, err
	}
	out, err := os.Create(cache)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(out, xzr); err != nil {
		_ = out.Close()
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	return parsePackagesFile(cache)
}

func (i *Installer) downloadPackage(ctx context.Context, base string, pkg Package) (string, error) {
	if pkg.Filename == "" {
		return "", fmt.Errorf("%s has no Filename field", pkg.Package)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, pkg.Filename)
	name := filepath.Base(pkg.Filename)
	dest := filepath.Join(i.opts.CacheDir, name)
	if pkg.SHA256 != "" {
		if err := verifyFileSHA256(dest, pkg.SHA256); err == nil {
			return dest, nil
		}
	}
	if err := i.downloadFile(ctx, u.String(), dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (i *Installer) downloadFile(ctx context.Context, source, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Debian APT-HTTP/1.3 (dmonitor-improved)")
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: %s", source, resp.Status)
	}
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func debianRepoBase() string {
	return "http://deb.debian.org/debian"
}

func verifyFileSHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("%s sha256 = %s, want %s", path, got, want)
	}
	return nil
}

type Package struct {
	Package      string
	Version      string
	Architecture string
	Filename     string
	SHA256       string
	Depends      string
}

func parsePackagesFile(path string) ([]Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var packages []Package
	current := map[string]string{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var lastKey string
	flush := func() {
		if len(current) == 0 {
			return
		}
		packages = append(packages, Package{
			Package:      current["Package"],
			Version:      current["Version"],
			Architecture: current["Architecture"],
			Filename:     current["Filename"],
			SHA256:       current["SHA256"],
			Depends:      current["Depends"],
		})
		current = map[string]string{}
		lastKey = ""
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, " ") && lastKey != "" {
			current[lastKey] += "\n" + strings.TrimSpace(line)
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		lastKey = key
		current[key] = strings.TrimSpace(value)
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return packages, nil
}

func findPackage(packages []Package, name, arch string) (Package, bool) {
	candidates := make([]Package, 0)
	for _, pkg := range packages {
		if pkg.Package == name && (pkg.Architecture == arch || pkg.Architecture == "all") {
			candidates = append(candidates, pkg)
		}
	}
	if len(candidates) == 0 {
		return Package{}, false
	}
	sort.Slice(candidates, func(a, b int) bool { return candidates[a].Version > candidates[b].Version })
	return candidates[0], true
}

var depVersionRE = regexp.MustCompile(`\s*\([^)]*\)`)

func dependencyNames(depends string) []string {
	if depends == "" {
		return nil
	}
	parts := strings.Split(depends, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		alts := strings.Split(part, "|")
		name := depVersionRE.ReplaceAllString(strings.TrimSpace(alts[0]), "")
		name = strings.TrimSpace(name)
		if base, _, ok := strings.Cut(name, ":"); ok {
			name = base
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
