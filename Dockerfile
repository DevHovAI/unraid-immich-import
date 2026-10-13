# Build
FROM docker.io/library/golang:1.24 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /camera-import ./cmd/camera-import

# Runtime: only the binary and the certificates for HTTPS (Telegram)
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /camera-import /camera-import
ENV CONFIG_DIR=/config DISKS_DIR=/mnt/disks
VOLUME /config
ENTRYPOINT ["/camera-import"]
