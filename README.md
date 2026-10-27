# McPanel

A simple, self-hosted web panel for running Minecraft servers. One Go binary,
no database. It downloads the server software and the right Java version for you, and gives you a clean page to start, stop, configure and back up your servers.

## Features

- **Server types:** Paper, Vanilla, Fabric (Java) and Bedrock.
- **Automatic setup:** downloads server files and the matching Eclipse Temurin
  Java runtime (e.g. Java 21 for 1.21, Java 25 for 26.x).
- **Live console** with command history.
- **Settings** as labelled controls, plus a raw `server.properties` editor that
  keeps your comments and ordering.
- **Players:** who's online, kick, operators, bans and the whitelist. Players
  turned away by the whitelist show up with an *Allow* button.
- **Backups:** manual or scheduled, made while the server keeps running. Restore
  takes a safety backup first. Scheduled backups are skipped when nobody has
  played since the last one.
- **Plugins & mods:** search Modrinth for things that work with your exact
  version. Downloads are checksum-verified and required dependencies come along.
- **Import:** upload a zip of a server from another host (Paper, Spigot, Bukkit,
  Purpur, Fabric, Vanilla or Bedrock) or of a single world. McPanel detects the
  type and version, lets you check them, and keeps worlds, configs, plugins and
  player lists.
- **Version changes** back up first and keep your worlds.
- **Crash restarts** and **start with the panel** per server.
- **Customisable:** panel name, accent colour, light/dark/system theme.
- **Two-factor sign-in** with any authenticator app, plus single-use recovery
  codes.
- Stops every server cleanly (saving worlds) when the panel shuts down.

## Quick start

Requirements: Linux (x86-64 or ARM64) and Go 1.26+ to build.

```sh
make build
./bin/mcpanel -data ./data
```

Open `http://<this machine>:8080`. On first launch the panel prints a **setup
code** in its log; enter it in the browser to choose your username and password.

### Options

| Flag | Env var | Default | |
|---|---|---|---|
| `-data` | `MCPANEL_DATA` | `./data` | Servers, backups, Java runtimes and settings |
| `-addr` | `MCPANEL_ADDR` | `:8080` | Listen address |
| `-tls-cert`, `-tls-key` | `MCPANEL_TLS_CERT`, `MCPANEL_TLS_KEY` | | Serve HTTPS directly |

Forgot the password or lost your 2FA device? Stop McPanel and run
`mcpanel reset-password -data ./data`. This also turns two-factor sign-in off,
and prints your username. Panels set up before usernames were added sign in as
`admin`.
In Docker: `docker compose run --rm mcpanel reset-password` (with the panel
stopped).

To run as a service, see [`deploy/mcpanel.service`](deploy/mcpanel.service).

## Docker

Images are published to `ghcr.io/revise0592/mcpanel` for x86-64 and ARM64
(`latest`, or a version such as `1.0.0`). With the included
[`compose.yaml`](compose.yaml):

```sh
docker compose up -d
docker compose logs mcpanel     # shows the first-time setup code
```

Then open `http://<host>:8080`. To build the image yourself instead, swap
`image:` for `build: .` in `compose.yaml`. Everything lives in `./data`, the same layout
as running the binary directly, so you can move between the two.

- **Ports:** the compose file maps the panel (8080), Java servers
  (25565–25575/tcp) and Bedrock servers (19132–19141/udp). New servers pick
  ports from these ranges automatically; widen them if you run more.
- **Stopping:** `docker compose down` gives servers 2 minutes to save their
  worlds. With plain `docker stop`, pass `-t 120`.
- **File ownership:** the container runs as UID/GID 1000. If `./data` is owned
  by another user, set `user:` in `compose.yaml`.
- **SELinux** (Fedora, RHEL): mount with `./data:/data:Z`.
- **Podman (rootless):** add `--userns=keep-id` so files in `./data` stay owned
  by you:

  ```sh
  podman run -d --name mcpanel --init --userns=keep-id \
    -p 8080:8080 -p 25565-25575:25565-25575 -p 19132-19141:19132-19141/udp \
    -v ./data:/data:Z ghcr.io/revise0592/mcpanel:latest
  ```

The image is Debian-based (not Alpine) because the Java runtimes McPanel
downloads and the Bedrock server need glibc.

## Data layout

```
data/
  mcpanel.json          panel settings and server list
  servers/<id>/         each server's files (worlds, configs, plugins)
  backups/<id>/         .tar.gz backups
  runtimes/java-<N>/    downloaded Java runtimes
```

## Notes

- **Bedrock:** if the automatic download from minecraft.net fails, download
  the Linux server zip yourself and use *Settings → Install from a zip*.
- **Playing with friends outside your network:** forward the server's port on
  your router (TCP for Java, UDP for Bedrock).
- The panel has a single admin account. If you expose it to the internet, put
  it behind HTTPS (`-tls-cert`/`-tls-key` or a reverse proxy).

## Development

```sh
make check   # gofmt, go vet, tests
```

Frontend: Go `html/template` with [htmx](https://htmx.org), all embedded in the
binary. No Node build step.

## Releases

Publishing a GitHub release with a tag like `v1.0.0` runs
[`release.yml`](.github/workflows/release.yml), which pushes the multi-arch
Docker image and attaches Linux binaries (`mcpanel-linux-amd64`,
`mcpanel-linux-arm64`) plus `SHA256SUMS` to the release.

## License

[MIT](LICENSE). Bundles [htmx](https://htmx.org) (0BSD) and uses
[rsc.io/qr](https://pkg.go.dev/rsc.io/qr) (BSD-3-Clause). Minecraft is a
trademark of Mojang; McPanel is not affiliated with Mojang or Microsoft.
