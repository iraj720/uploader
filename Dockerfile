FROM golang:1.22-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends libsqlite3-dev gcc && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux \
    go build -ldflags="-s -w" -o /workspace/uploader ./cmd/uploader

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates libsqlite3-0 && \
    rm -rf /var/lib/apt/lists/*

RUN addgroup --system uploader && adduser --system --ingroup uploader uploader

COPY --from=builder /workspace/uploader /usr/local/bin/uploader
COPY config.yaml /etc/uploader/config.yaml

WORKDIR /etc/uploader

USER uploader

ENTRYPOINT ["/usr/local/bin/uploader"]
