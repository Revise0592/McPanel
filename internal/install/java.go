package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/Revise0592/McPanel/internal/archive"
)

var javaMu sync.Mutex

// JavaPath returns the java binary for a managed runtime of the given major
// version, downloading Eclipse Temurin if needed.
func JavaPath(ctx context.Context, dataDir string, major int, logf Logf) (string, error) {
	javaMu.Lock()
	defer javaMu.Unlock()
	dir := filepath.Join(dataDir, "runtimes", fmt.Sprintf("java-%d", major))
	bin := filepath.Join(dir, "bin", "java")
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}
	arch := map[string]string{"amd64": "x64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return "", fmt.Errorf("no Java download available for %s", runtime.GOARCH)
	}
	logf("Downloading Java %d runtime", major)
	url := fmt.Sprintf("https://api.adoptium.net/v3/binary/latest/%d/ga/linux/%s/jre/hotspot/normal/eclipse", major, arch)
	tgz := filepath.Join(dataDir, "runtimes", fmt.Sprintf("java-%d.tar.gz", major))
	if err := Download(ctx, url, tgz, Checksum{}, logf); err != nil {
		return "", fmt.Errorf("download Java %d: %w", major, err)
	}
	defer os.Remove(tgz)
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := archive.ExtractTarGz(tgz, tmp, 1); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("unpack Java %d: %w", major, err)
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	logf("Java %d ready", major)
	return bin, nil
}
