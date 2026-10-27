# Contributing

## Development

```sh
make check   # gofmt, go vet, tests
make build   # builds ./bin/mcpanel
```

Frontend: Go `html/template` with [htmx](https://htmx.org), all embedded in the
binary. No Node build step.

## Releases

Publishing a GitHub release with a tag like `v1.0.0` runs
[`release.yml`](.github/workflows/release.yml). It pushes the multi-arch Docker
image (`ghcr.io/revise0592/mcpanel`, tagged `latest` and the version) and
attaches Linux binaries (`mcpanel-linux-amd64`, `mcpanel-linux-arm64`) plus
`SHA256SUMS` to the release.
