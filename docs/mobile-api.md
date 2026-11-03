# Mobile app API

McPanel has a small JSON API for a companion phone app. It lives under
`/api/v1` and is separate from the web pages. It never reads the browser's
sign-in cookie.

## Pairing

1. In the panel, open **Settings → Mobile app** and press **Pair a phone**.
   The panel shows a QR code and an 8-character code (`ABCD-EFGH`). The
   code works once and expires after 10 minutes. Restarting the panel
   cancels it.
2. The QR code holds a link:

   ```
   mcpanel://pair?url=https%3A%2F%2Fpanel.example.com&code=ABCDEFGH
   ```

   If the user can't scan it, they can type in the address and the code
   instead.
3. The app sends the code to the panel:

   ```http
   POST /api/v1/pair
   Content-Type: application/json

   {"code": "ABCD-EFGH", "name": "Pixel 9"}
   ```

   Codes are accepted with or without the dash, in either case. `name` is
   shown in the panel's list of phones (up to 40 characters; defaults to
   "Phone").

   ```json
   201 Created
   {
     "token": "mcp_…",
     "device": {"id": "a251fa66fe61", "name": "Pixel 9", "createdAt": "…"},
     "panel": {"name": "McPanel", "accent": "#3b82f6", "version": "1.4.0", "apiVersion": 1}
   }
   ```

   The token is shown only once; the panel keeps a hash of it. Store it
   securely, for example with Android Keystore or EncryptedSharedPreferences.
   Ten wrong codes from one address lock that address out for 15 minutes
   (`429`).

Every other request sends the token:

```
Authorization: Bearer mcp_…
```

A token stops working when the phone is removed in Settings, when the
app calls `POST /api/v1/unpair`, or when the panel's password is changed
(also with `mcpanel reset-password`). The API then answers `401`, and the
app should ask the user to pair again.

## Errors

Errors have a status code and a message that can be shown to the user:

```json
{"error": "The server isn't running."}
```

| Status | Meaning |
|---|---|
| 400 | Bad request body or query |
| 401 | Missing, wrong or removed token; or wrong pairing code |
| 404 | No such server, action or address |
| 409 | The action can't be done right now (server not running, not installed, …) |
| 429 | Too many wrong pairing codes |

## Endpoints

| Method and path | What it does |
|---|---|
| `GET /api/v1/me` | This device and the panel's name, accent colour and version |
| `POST /api/v1/unpair` | Removes this device. `204` |
| `GET /api/v1/servers` | Every server, sorted by name |
| `GET /api/v1/servers/{id}` | One server |
| `POST /api/v1/servers/{id}/power/{action}` | `start`, `stop`, `restart` or `kill`. Returns the server |
| `POST /api/v1/servers/{id}/command` | Body `{"command": "say hi"}`. A leading `/` is dropped. `204` |
| `GET /api/v1/servers/{id}/console` | Live console as server-sent events (see below) |
| `GET /api/v1/servers/{id}/activity?limit=50` | The server's history, newest first |
| `GET /api/v1/servers/{id}/usage?range=hour` | CPU, memory and TPS samples (`hour` or `day`), oldest first |
| `GET /api/v1/activity?since=…&limit=50` | Recent events from every server, newest first |

`limit` is at most 200.

### Server

```json
{
  "id": "survival-3fa2c1",
  "name": "Survival",
  "type": "paper",
  "typeLabel": "Paper",
  "version": "1.21.4",
  "installed": true,
  "state": "running",
  "label": "Online",
  "task": "",
  "progress": 0,
  "startedAt": "2026-11-03T07:00:00Z",
  "players": ["Steve"],
  "address": "mc.example.com:25565",
  "bedrockAddress": "mc.example.com port 19132",
  "memoryLimitMB": 4096,
  "usage": {"cpu": 12.5, "memoryMB": 2048, "tps": 19.98},
  "newBuild": "",
  "autoStart": true,
  "restartOnCrash": true,
  "sleepAfterMins": 0
}
```

- `state` is one of `stopped`, `starting`, `running`, `stopping`,
  `crashed` or `sleeping`.
- `label` is the panel's own wording for the state. While an install or
  backup runs, `label` is that task and `progress` is its percentage (`-1`
  when unknown).
- `startedAt` is only there while the server is up.
- `usage` is only there once the panel has measured it. `cpu` is `-1` until
  there are two measurements, and `tps` is only reported by Paper and its
  forks.
- Fields with no value, such as `bedrockAddress`, `task` and `newBuild`, may
  be left out.

### Events

```json
{"serverId": "survival-3fa2c1", "serverName": "Survival",
 "time": "2026-11-03T08:01:12Z", "kind": "crash", "text": "Server exited unexpectedly", "repeats": 2}
```

`kind` is `crash`, `failed`, `update` or `power`, or is left out for plain
events such as a finished backup. `serverId` and `serverName` are only in
`/api/v1/activity`, which covers every server. Pass the `time` of the newest
event you've seen as `since` (RFC 3339) to get only newer ones. Until push
notifications exist, an app can poll this to notify the user.

### Console stream

`GET /api/v1/servers/{id}/console` is a `text/event-stream`. It sends the
recent backlog first, then each new line as one `data:` event. A `: ping`
comment arrives every 25 seconds to keep the connection open. On Android,
OkHttp's `okhttp-sse` can send the `Authorization` header, which the
browser's `EventSource` can't.

## Notes

- Use HTTPS, either directly (`-tls-cert`/`-tls-key`) or through a reverse
  proxy. Over plain HTTP the token is readable to anyone on the network.
  The pairing page warns when it is opened over HTTP.
- The QR code uses the address the browser used to open the pairing page.
  To pair a phone that will be used away from home, open the panel through
  its public address first.
- Requests with an `Origin` header from another site are refused, the same
  as for the web pages. Native apps don't send one.
