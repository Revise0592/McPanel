package install

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// --- Purpur ---

func purpurVersions(ctx context.Context) ([]string, error) {
	var p struct {
		Versions []string `json:"versions"`
	}
	if err := GetJSON(ctx, "https://api.purpurmc.org/v2/purpur", &p); err != nil {
		return nil, err
	}
	var out []string
	for _, v := range p.Versions {
		if !strings.Contains(v, "-") {
			out = append(out, v)
		}
	}
	SortVersionsDesc(out)
	return out, nil
}

type purpurBuild struct {
	Build  string `json:"build"`
	Result string `json:"result"`
	MD5    string `json:"md5"`
}

func latestPurpurBuild(ctx context.Context, version string) (purpurBuild, error) {
	var b purpurBuild
	err := GetJSON(ctx, fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/latest", version), &b)
	if err == nil && b.Build == "" {
		err = fmt.Errorf("no Purpur build for %s", version)
	}
	return b, err
}

func installPurpur(ctx context.Context, version, dir string, logf Logf) (Result, error) {
	b, err := latestPurpurBuild(ctx, version)
	if err != nil {
		return Result{}, err
	}
	major, err := JavaMajorFor(ctx, version)
	if err != nil {
		return Result{}, err
	}
	logf("Downloading Purpur %s build %s", version, b.Build)
	url := fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/%s/download", version, b.Build)
	err = Download(ctx, url, filepath.Join(dir, "server.jar"), Checksum{"md5", b.MD5}, logf)
	return Result{Version: version, Build: b.Build, Executable: "server.jar", JavaMajor: major}, err
}

// --- Velocity ---

// VelocityJar is the proxy's program in its folder.
const VelocityJar = "velocity.jar"

func installVelocity(ctx context.Context, version, dir string, logf Logf) (Result, error) {
	var info struct {
		Version struct {
			Java struct {
				Version struct {
					Minimum int `json:"minimum"`
				} `json:"version"`
			} `json:"java"`
		} `json:"version"`
	}
	if err := GetJSON(ctx, "https://fill.papermc.io/v3/projects/velocity/versions/"+version, &info); err != nil {
		return Result{}, err
	}
	major := info.Version.Java.Version.Minimum
	if major == 0 {
		major = 21
	}
	b, err := downloadFill(ctx, "velocity", version, filepath.Join(dir, VelocityJar), logf)
	return Result{Version: version, Build: strconv.Itoa(b.ID), Executable: VelocityJar, JavaMajor: major}, err
}

// --- NeoForge ---

const neoMaven = "https://maven.neoforged.net/releases/net/neoforged/neoforge/"

// neoForgeMC returns the Minecraft version a NeoForge release is for.
// Releases for 1.20.2 to 1.21.x drop the leading "1." (21.1.77 is for
// 1.21.1, 21.0.x for 1.21). From Minecraft 26.1 on, the whole Minecraft
// version comes first, with .0 standing for no patch (26.3.0.84 is for 26.3).
func neoForgeMC(v string) (mc string, beta, ok bool) {
	base, suffix, _ := strings.Cut(v, "-")
	beta = suffix != ""
	parts := strings.Split(base, ".")
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false, false
		}
	}
	major, _ := strconv.Atoi(parts[0])
	switch {
	case major >= 26 && len(parts) == 4:
		if parts[2] == "0" {
			return parts[0] + "." + parts[1], beta, true
		}
		return strings.Join(parts[:3], "."), beta, true
	case major >= 20 && major < 26 && len(parts) == 3:
		if parts[1] == "0" {
			return "1." + parts[0], beta, true
		}
		return "1." + parts[0] + "." + parts[1], beta, true
	}
	return "", false, false
}

// newerNeoForge reports whether a should be picked over b: stable releases
// win over betas, then the higher version.
func newerNeoForge(a, b string) bool {
	if b == "" {
		return true
	}
	aBase, aSuf, _ := strings.Cut(a, "-")
	bBase, bSuf, _ := strings.Cut(b, "-")
	if (aSuf == "") != (bSuf == "") {
		return aSuf == ""
	}
	return CompareVersions(aBase, bBase) > 0
}

// neoForgeIndex maps each Minecraft version to its best NeoForge release.
func neoForgeIndex(ctx context.Context) (map[string]string, error) {
	resp, err := get(ctx, neoMaven+"maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var meta struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&meta); err != nil {
		return nil, err
	}
	best := map[string]string{}
	for _, v := range meta.Versions {
		mc, _, ok := neoForgeMC(v)
		if ok && newerNeoForge(v, best[mc]) {
			best[mc] = v
		}
	}
	return best, nil
}

func neoForgeVersions(ctx context.Context) ([]string, error) {
	idx, err := neoForgeIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(idx))
	for mc := range idx {
		out = append(out, mc)
	}
	SortVersionsDesc(out)
	return out, nil
}

func latestNeoForge(ctx context.Context, mc string) (string, error) {
	idx, err := neoForgeIndex(ctx)
	if err != nil {
		return "", err
	}
	if v := idx[mc]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no NeoForge release for Minecraft %s", mc)
}

func installNeoForge(ctx context.Context, dataDir, mc, v, dir string, logf Logf) (Result, error) {
	var err error
	if v == "" {
		if v, err = latestNeoForge(ctx, mc); err != nil {
			return Result{}, err
		}
	}
	url := fmt.Sprintf("%s%s/neoforge-%s-installer.jar", neoMaven, v, v)
	args := fmt.Sprintf("libraries/net/neoforged/neoforge/%s/unix_args.txt", v)
	return runInstaller(ctx, dataDir, "NeoForge", mc, v, url, args, "", dir, logf)
}

// --- Forge ---

const forgeMaven = "https://maven.minecraftforge.net/net/minecraftforge/forge/"

// oldestForge is the oldest Minecraft version offered. Older Forge
// installers work differently and are rarely wanted for a new server.
const oldestForge = "1.12.2"

// forgeIndex maps each Minecraft version to its recommended Forge release,
// or the latest when none is recommended.
func forgeIndex(ctx context.Context) (map[string]string, error) {
	var p struct {
		Promos map[string]string `json:"promos"`
	}
	if err := GetJSON(ctx, "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &p); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range p.Promos {
		mc, kind, ok := strings.Cut(k, "-")
		if !ok || strings.ContainsAny(mc, "_-") || CompareVersions(mc, oldestForge) < 0 {
			continue
		}
		if kind == "recommended" || out[mc] == "" {
			out[mc] = v
		}
	}
	return out, nil
}

func forgeVersions(ctx context.Context) ([]string, error) {
	idx, err := forgeIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(idx))
	for mc := range idx {
		out = append(out, mc)
	}
	SortVersionsDesc(out)
	return out, nil
}

func latestForge(ctx context.Context, mc string) (string, error) {
	idx, err := forgeIndex(ctx)
	if err != nil {
		return "", err
	}
	if v := idx[mc]; v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no Forge release for Minecraft %s", mc)
}

func installForge(ctx context.Context, dataDir, mc, v, dir string, logf Logf) (Result, error) {
	var err error
	if v == "" {
		if v, err = latestForge(ctx, mc); err != nil {
			return Result{}, err
		}
	}
	full := mc + "-" + v
	url := fmt.Sprintf("%s%s/forge-%s-installer.jar", forgeMaven, full, full)
	// Forge for 1.17 and later starts from an argument file like NeoForge;
	// older versions make a jar to run.
	args := fmt.Sprintf("libraries/net/minecraftforge/forge/%s/unix_args.txt", full)
	return runInstaller(ctx, dataDir, "Forge", mc, v, url, args, "forge-"+full, dir, logf)
}

// runInstaller downloads a NeoForge or Forge installer and runs it in dir.
// The server then starts from the argument file args, or, when the
// installer made none, from a jar whose name starts with jarPrefix.
func runInstaller(ctx context.Context, dataDir, label, mc, v, url, args, jarPrefix, dir string, logf Logf) (Result, error) {
	major, err := JavaMajorFor(ctx, mc)
	if err != nil {
		return Result{}, err
	}
	java, err := JavaPath(ctx, dataDir, major, logf)
	if err != nil {
		return Result{}, err
	}
	jar := filepath.Join(dir, "mcpanel-installer.jar")
	logf("Downloading the %s %s installer", label, v)
	if err := Download(ctx, url, jar, Checksum{}, logf); err != nil {
		return Result{}, err
	}
	defer os.Remove(jar)
	defer os.Remove(jar + ".log")

	logf("Running the %s installer. It downloads Minecraft and the libraries %s needs, which takes a minute or two.", label, label)
	cmd := exec.CommandContext(ctx, java, "-jar", filepath.Base(jar), "--installServer")
	cmd.Dir = dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	// The installer lists every file it fetches; show a count instead,
	// and the last lines if it fails.
	var tail []string
	lines, last := 0, time.Now()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		lines++
		tail = append(tail, sc.Text())
		if len(tail) > 15 {
			tail = tail[1:]
		}
		if time.Since(last) > 3*time.Second {
			last = time.Now()
			logf("%s installer is working (%d steps so far)", label, lines)
		}
	}
	if err := cmd.Wait(); err != nil {
		for _, l := range tail {
			logf("  %s", l)
		}
		return Result{}, fmt.Errorf("the %s installer failed: %w", label, err)
	}

	exe := ""
	if _, err := os.Stat(filepath.Join(dir, args)); err == nil {
		exe = "@" + args
	} else if jarPrefix != "" {
		matches, _ := filepath.Glob(filepath.Join(dir, jarPrefix+"*.jar"))
		for _, m := range matches {
			if !strings.HasSuffix(m, "-installer.jar") {
				exe = filepath.Base(m)
				break
			}
		}
	}
	if exe == "" {
		return Result{}, fmt.Errorf("the %s installer finished, but McPanel couldn't find how to start the server", label)
	}
	os.MkdirAll(filepath.Join(dir, "mods"), 0o755)
	return Result{Version: mc, Build: v, Executable: exe, JavaMajor: major}, nil
}
