// Package install downloads server software and Java runtimes.
package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/archive"
	"github.com/Revise0592/McPanel/internal/store"
)

// Result describes what was installed.
type Result struct {
	Version    string
	Build      string
	Executable string
	JavaMajor  int
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cached{}
)

type cached struct {
	at   time.Time
	list []string
}

// Versions lists installable versions for a server type, newest first.
func Versions(ctx context.Context, typ string) ([]string, error) {
	cacheMu.Lock()
	c, ok := cache[typ]
	cacheMu.Unlock()
	if ok && time.Since(c.at) < 15*time.Minute {
		return c.list, nil
	}
	var list []string
	var err error
	switch typ {
	case store.TypeVanilla:
		list, err = vanillaVersions(ctx)
	case store.TypePaper:
		list, err = paperVersions(ctx)
	case store.TypeFabric:
		list, err = fabricVersions(ctx)
	case store.TypeBedrock:
		list = []string{"latest"}
	default:
		err = fmt.Errorf("unknown server type %q", typ)
	}
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	cache[typ] = cached{time.Now(), list}
	cacheMu.Unlock()
	return list, nil
}

// Install downloads the server software for typ/version into dir. Existing
// worlds and settings are kept.
func Install(ctx context.Context, dataDir, typ, version, dir string, logf Logf) (Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	var res Result
	var err error
	switch typ {
	case store.TypeVanilla:
		res, err = installVanilla(ctx, version, dir, logf)
	case store.TypePaper:
		res, err = installPaper(ctx, version, dir, logf)
	case store.TypeFabric:
		res, err = installFabric(ctx, version, dir, logf)
	case store.TypeBedrock:
		res, err = installBedrock(ctx, dir, logf)
	default:
		err = fmt.Errorf("unknown server type %q", typ)
	}
	if err != nil {
		return res, err
	}
	if store.IsJava(typ) {
		if _, err := JavaPath(ctx, dataDir, res.JavaMajor, logf); err != nil {
			return res, err
		}
		if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
			return res, err
		}
	}
	return res, nil
}

// LatestBuild returns the newest build that reinstalling sv's current
// version would get: the Paper build number, the Fabric loader version, or the
// Bedrock release. It returns "" for Vanilla, which has no builds.
func LatestBuild(ctx context.Context, sv store.Server) (string, error) {
	switch sv.Type {
	case store.TypePaper:
		b, err := latestPaperBuild(ctx, sv.Version)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(b.ID), nil
	case store.TypeFabric:
		return latestStable(ctx, "https://meta.fabricmc.net/v2/versions/loader")
	case store.TypeBedrock:
		_, v, err := bedrockDownload(ctx)
		return v, err
	}
	return "", nil
}

// IsNewerBuild reports whether latest (from LatestBuild) is newer than what
// sv has installed.
func IsNewerBuild(sv store.Server, latest string) bool {
	if latest == "" || latest == "unknown" {
		return false
	}
	current := sv.Build
	if sv.Type == store.TypeBedrock {
		current = sv.Version
		if current == "unknown" || current == "latest" {
			return false
		}
	}
	if current == "" {
		return false
	}
	return CompareVersions(latest, current) > 0
}

// --- Mojang / Vanilla ---

type manifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}

type versionInfo struct {
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads struct {
		Server *struct {
			SHA1 string `json:"sha1"`
			URL  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
}

func getManifest(ctx context.Context) (manifest, error) {
	var m manifest
	err := GetJSON(ctx, "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &m)
	return m, err
}

func vanillaVersions(ctx context.Context) ([]string, error) {
	m, err := getManifest(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range m.Versions {
		if v.Type == "release" {
			out = append(out, v.ID)
		}
	}
	return out, nil
}

func getVersionInfo(ctx context.Context, version string) (versionInfo, error) {
	var vi versionInfo
	m, err := getManifest(ctx)
	if err != nil {
		return vi, err
	}
	for _, v := range m.Versions {
		if v.ID == version {
			if err := GetJSON(ctx, v.URL, &vi); err != nil {
				return vi, err
			}
			if vi.JavaVersion.MajorVersion == 0 {
				vi.JavaVersion.MajorVersion = 8
			}
			return vi, nil
		}
	}
	return vi, fmt.Errorf("Minecraft version %s not found", version)
}

// JavaMajorFor returns the Java version a Minecraft release needs.
func JavaMajorFor(ctx context.Context, version string) (int, error) {
	vi, err := getVersionInfo(ctx, version)
	return vi.JavaVersion.MajorVersion, err
}

func installVanilla(ctx context.Context, version, dir string, logf Logf) (Result, error) {
	vi, err := getVersionInfo(ctx, version)
	if err != nil {
		return Result{}, err
	}
	if vi.Downloads.Server == nil {
		return Result{}, fmt.Errorf("version %s has no server download", version)
	}
	logf("Downloading Minecraft %s server", version)
	err = Download(ctx, vi.Downloads.Server.URL, filepath.Join(dir, "server.jar"),
		Checksum{"sha1", vi.Downloads.Server.SHA1}, logf)
	return Result{Version: version, Executable: "server.jar", JavaMajor: vi.JavaVersion.MajorVersion}, err
}

// --- Paper ---

func paperVersions(ctx context.Context) ([]string, error) {
	var p struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := GetJSON(ctx, "https://fill.papermc.io/v3/projects/paper", &p); err != nil {
		return nil, err
	}
	var out []string
	for _, vs := range p.Versions {
		for _, v := range vs {
			if !strings.Contains(v, "-") {
				out = append(out, v)
			}
		}
	}
	SortVersionsDesc(out)
	return out, nil
}

type paperBuild struct {
	ID        int `json:"id"`
	Downloads map[string]struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Checksums struct {
			SHA256 string `json:"sha256"`
		} `json:"checksums"`
	} `json:"downloads"`
}

func latestPaperBuild(ctx context.Context, version string) (paperBuild, error) {
	var b paperBuild
	err := GetJSON(ctx, fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds/latest", version), &b)
	return b, err
}

func installPaper(ctx context.Context, version, dir string, logf Logf) (Result, error) {
	b, err := latestPaperBuild(ctx, version)
	if err != nil {
		return Result{}, err
	}
	d, ok := b.Downloads["server:default"]
	if !ok {
		return Result{}, fmt.Errorf("no Paper download for %s", version)
	}
	major, err := JavaMajorFor(ctx, version)
	if err != nil {
		return Result{}, err
	}
	logf("Downloading Paper %s build %d", version, b.ID)
	err = Download(ctx, d.URL, filepath.Join(dir, "server.jar"), Checksum{"sha256", d.Checksums.SHA256}, logf)
	return Result{Version: version, Build: strconv.Itoa(b.ID), Executable: "server.jar", JavaMajor: major}, err
}

// --- Fabric ---

type fabricMeta struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func fabricVersions(ctx context.Context) ([]string, error) {
	var vs []fabricMeta
	if err := GetJSON(ctx, "https://meta.fabricmc.net/v2/versions/game", &vs); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range vs {
		if v.Stable {
			out = append(out, v.Version)
		}
	}
	return out, nil
}

func latestStable(ctx context.Context, url string) (string, error) {
	var vs []fabricMeta
	if err := GetJSON(ctx, url, &vs); err != nil {
		return "", err
	}
	for _, v := range vs {
		if v.Stable {
			return v.Version, nil
		}
	}
	return "", fmt.Errorf("no stable release at %s", url)
}

func installFabric(ctx context.Context, version, dir string, logf Logf) (Result, error) {
	loader, err := latestStable(ctx, "https://meta.fabricmc.net/v2/versions/loader")
	if err != nil {
		return Result{}, err
	}
	installer, err := latestStable(ctx, "https://meta.fabricmc.net/v2/versions/installer")
	if err != nil {
		return Result{}, err
	}
	major, err := JavaMajorFor(ctx, version)
	if err != nil {
		return Result{}, err
	}
	logf("Downloading Fabric loader %s for Minecraft %s", loader, version)
	url := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar", version, loader, installer)
	err = Download(ctx, url, filepath.Join(dir, "fabric-server-launch.jar"), Checksum{}, logf)
	if err == nil {
		os.MkdirAll(filepath.Join(dir, "mods"), 0o755)
	}
	return Result{Version: version, Build: loader, Executable: "fabric-server-launch.jar", JavaMajor: major}, err
}

// --- Bedrock ---

var bedrockVersionRe = regexp.MustCompile(`bedrock-server-([0-9.]+)\.zip`)

// bedrockKeep lists files a Bedrock update must not overwrite.
var bedrockKeep = map[string]bool{
	"server.properties": true,
	"allowlist.json":    true,
	"permissions.json":  true,
}

func bedrockDownload(ctx context.Context) (url, version string, err error) {
	var links struct {
		Result struct {
			Links []struct {
				DownloadType string `json:"downloadType"`
				DownloadURL  string `json:"downloadUrl"`
			} `json:"links"`
		} `json:"result"`
	}
	if err := GetJSON(ctx, "https://net-secondary.web.minecraft-services.net/api/v1.0/download/links", &links); err != nil {
		return "", "", err
	}
	for _, l := range links.Result.Links {
		if l.DownloadType == "serverBedrockLinux" {
			url = l.DownloadURL
		}
	}
	if url == "" {
		return "", "", fmt.Errorf("no Bedrock server download found")
	}
	return url, BedrockZipVersion(url), nil
}

func installBedrock(ctx context.Context, dir string, logf Logf) (Result, error) {
	url, version, err := bedrockDownload(ctx)
	if err != nil {
		return Result{}, err
	}
	logf("Downloading Bedrock server %s", version)
	zip := filepath.Join(dir, "bedrock-server.zip")
	if err := Download(ctx, url, zip, Checksum{}, logf); err != nil {
		return Result{}, fmt.Errorf("%w (you can download the server zip from minecraft.net yourself and upload it on the Settings tab)", err)
	}
	defer os.Remove(zip)
	return InstallBedrockZip(zip, version, dir, logf)
}

// InstallBedrockZip unpacks a Bedrock server zip into dir, keeping existing
// settings and permission files.
func InstallBedrockZip(zip, version, dir string, logf Logf) (Result, error) {
	logf("Unpacking Bedrock server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, err
	}
	err := archive.ExtractZip(zip, dir, func(name string) bool {
		if !bedrockKeep[name] {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("that doesn't look like a Bedrock server zip: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bedrock_server")); err != nil {
		return Result{}, errors.New("the zip has no bedrock_server program; make sure it's the Linux version")
	}
	os.Chmod(filepath.Join(dir, "bedrock_server"), 0o755)
	return Result{Version: version, Executable: "bedrock_server"}, nil
}

// BedrockZipVersion extracts a version number from a Bedrock zip file name.
func BedrockZipVersion(name string) string {
	if m := bedrockVersionRe.FindStringSubmatch(name); m != nil {
		return m[1]
	}
	return "unknown"
}

// --- helpers ---

// SortVersionsDesc sorts dotted version strings newest first.
func SortVersionsDesc(vs []string) {
	sort.Slice(vs, func(i, j int) bool { return CompareVersions(vs[i], vs[j]) > 0 })
}

// CompareVersions compares dotted numeric versions.
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
