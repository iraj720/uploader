FROM golang:1.22-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /workspace/uploader ./cmd/uploader

FROM alpine:3.19

RUN apk add --no-cache ca-certificates \
 && addgroup -S uploader \
 && adduser -S uploader -G uploader

COPY --from=builder /workspace/uploader /usr/local/bin/uploader
COPY config.yaml /etc/uploader/config.yaml

WORKDIR /etc/uploader

USER uploader

ENTRYPOINT ["/usr/local/bin/uploader"]
