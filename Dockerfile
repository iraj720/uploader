FROM golang:1.22-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends libsqlite3-dev && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /workspace/uploader ./cmd/uploader

FROM gcr.io/distroless/cc-debian11

COPY --from=builder /workspace/uploader /usr/local/bin/uploader
COPY --from=builder /usr/lib/x86_64-linux-gnu/libsqlite3.so.0 /usr/lib/x86_64-linux-gnu/
COPY config.yaml /etc/uploader/config.yaml

WORKDIR /etc/uploader

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/uploader"]
