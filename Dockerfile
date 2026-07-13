FROM golang:1.25 AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /home-store ./cmd/home-store

FROM scratch

COPY --from=build /home-store /home-store
USER 65532:65532
EXPOSE 9000
VOLUME ["/data"]
ENTRYPOINT ["/home-store"]
CMD ["-addr", "0.0.0.0:9000", "-data-dir", "/data"]
