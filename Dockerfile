# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS web-builder

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.22-bookworm AS go-builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
RUN apt-get update \
  && apt-get install -y --no-install-recommends gcc-arm-linux-gnueabihf libc6-dev-armhf-cross \
  && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY compat/ ./compat/

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/dmonitor-improved ./cmd/dmonitor-improved \
  && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/dmonitor-install ./cmd/dmonitor-install \
  && arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -o /out/dmonitor-compat.so compat/dmonitor_compat.c -ldl

FROM debian:bookworm-slim AS runtime

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates gnupg qemu-user \
  && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /out/dmonitor-improved /usr/local/bin/dmonitor-improved
COPY --from=go-builder /out/dmonitor-install /usr/local/bin/dmonitor-install
COPY --from=go-builder /out/dmonitor-compat.so /usr/share/dmonitor-improved/dmonitor-compat.so
COPY --from=web-builder /src/web/dist /usr/share/dmonitor-improved/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint

ENV DMONITOR_ADDR=0.0.0.0:8080 \
  DMONITOR_ROOTFS=/var/lib/dmonitor/rootfs \
  DMONITOR_CACHE=/var/cache/dmonitor \
  DMONITOR_STATIC=/usr/share/dmonitor-improved/web \
  DMONITOR_QEMU=qemu-arm \
  DMONITOR_PRELOAD=/usr/lib/dmonitor-compat.so \
  DMONITOR_LOG_LEVEL=info \
  DMONITOR_AUTO_INSTALL=1

VOLUME ["/var/lib/dmonitor/rootfs", "/var/cache/dmonitor"]
EXPOSE 8080

ENTRYPOINT ["sh", "/usr/local/bin/docker-entrypoint"]
