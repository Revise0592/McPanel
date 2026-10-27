# syntax=docker/dockerfile:1

# --- build ---
# Runs on the build machine's own architecture and cross-compiles, which is
# much faster than emulating ARM for multi-arch images.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/mcpanel ./cmd/mcpanel

# --- runtime ---
# Debian rather than Alpine: the Java runtimes McPanel downloads and the
# Bedrock server are both built against glibc. curl brings libcurl, which
# Bedrock needs, and is used for the health check.
FROM docker.io/library/debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 1000 --user-group --home-dir /data --no-create-home mcpanel \
 && mkdir /data && chown mcpanel:mcpanel /data

COPY --from=build /out/mcpanel /usr/local/bin/mcpanel

ENV MCPANEL_DATA=/data \
    MCPANEL_ADDR=:8080
VOLUME /data
WORKDIR /data
USER mcpanel

# Panel, Java servers, Bedrock servers.
EXPOSE 8080/tcp 25565-25575/tcp 19132-19141/udp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD curl -fsS -o /dev/null http://127.0.0.1:8080/login || exit 1

# McPanel stops every game server on SIGTERM so worlds are saved; give it
# time with `docker stop -t 120` or stop_grace_period in compose.
STOPSIGNAL SIGTERM
ENTRYPOINT ["mcpanel"]
