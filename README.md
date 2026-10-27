# McPanel

A simple, self-hosted web panel for running Minecraft servers. It downloads the
server software and the right Java version for you, then lets you start, stop,
configure and back up your servers from one page.

## Features

- Paper, Vanilla, Fabric and Bedrock servers
- Java is downloaded automatically
- Live console
- Settings as plain controls, or edit `server.properties` directly
- Server icons
- Players: who's online, kick, ops, bans and whitelist
- Backups: manual or scheduled, while the server runs
- Plugins and mods from Modrinth, filtered to your version
- Import a server or world from a zip
- Restart on crash, start with the panel
- Two-factor sign-in
- Light/dark theme and accent colour

## Docker

```sh
docker compose up -d
docker compose logs mcpanel     # shows the first-time setup code
```

Open `http://<host>:8080` and enter the setup code to create your login.
Everything is stored in `./data`.

- **Ports:** 8080 for the panel, 25565–25575/tcp for Java servers,
  19132–19141/udp for Bedrock.
- **Stopping:** give servers time to save. `docker compose down` does this; with
  `docker stop`, add `-t 120`.
- **File ownership:** the container runs as UID 1000. Use `user:` in
  `compose.yaml` if `./data` belongs to someone else.
- **SELinux** (Fedora, RHEL): mount with `./data:/data:Z`.
- **Podman (rootless):** add `--userns=keep-id`.

### UnRAID

Image: `ghcr.io/revise0592/mcpanel:latest`. Map `/data` to an appdata folder and
add the ports above. In **Extra Parameters**:

```
--init --stop-timeout=120 --user 99:100
```

On a custom network such as `br0`, add `--dns=1.1.1.1` (or your router's IP) if
the panel can't load version lists or downloads.

## Without Docker

Needs Linux (x86-64 or ARM64). Download a binary from
[Releases](../../releases), or build with Go 1.26+:

```sh
make build
./bin/mcpanel -data ./data
```

The setup code is printed in the log. To run it as a service, see
[`deploy/mcpanel.service`](deploy/mcpanel.service).

| Flag | Env var | Default | |
|---|---|---|---|
| `-data` | `MCPANEL_DATA` | `./data` | Where everything is stored |
| `-addr` | `MCPANEL_ADDR` | `:8080` | Listen address |
| `-tls-cert`, `-tls-key` | `MCPANEL_TLS_CERT`, `MCPANEL_TLS_KEY` | | Serve HTTPS directly |

## Locked out?

Stop McPanel, then run `mcpanel reset-password -data ./data` (Docker:
`docker compose run --rm mcpanel reset-password`). This sets a new password,
turns two-factor sign-in off and shows your username.

## Good to know

- **Playing outside your network:** forward the server's port on your router
  (TCP for Java, UDP for Bedrock).
- **Bedrock download fails:** get the Linux server zip from minecraft.net and
  use *Settings → Install from a zip*.
- **Exposing the panel to the internet:** put it behind HTTPS, either with
  `-tls-cert`/`-tls-key` or a reverse proxy.

```
data/
  mcpanel.json        panel settings and server list
  servers/<id>/       server files (worlds, configs, plugins)
  backups/<id>/       backups
  runtimes/           downloaded Java
```

## License

[MIT](LICENSE). Bundles [htmx](https://htmx.org) (0BSD) and uses
[rsc.io/qr](https://pkg.go.dev/rsc.io/qr) (BSD-3-Clause). Not affiliated with
Mojang or Microsoft. Development notes are in [CONTRIBUTING.md](CONTRIBUTING.md).
