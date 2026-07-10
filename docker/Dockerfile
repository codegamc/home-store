# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/home-store ./cmd/home-store

FROM alpine:3.22
RUN addgroup -S -g 65532 homestore && adduser -S -D -H -u 65532 -G homestore homestore \
    && mkdir -p /var/lib/home-store/data \
    && chown -R homestore:homestore /var/lib/home-store
COPY --from=build /out/home-store /usr/local/bin/home-store
USER homestore
EXPOSE 9000
VOLUME ["/var/lib/home-store"]
ENV HOME_STORE_ADDR=0.0.0.0:9000 \
    HOME_STORE_DATA_DIR=/var/lib/home-store/data \
    HOME_STORE_DB_PATH=/var/lib/home-store/metadata.sqlite
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:9000/health/ready || exit 1
ENTRYPOINT ["/usr/local/bin/home-store"]
