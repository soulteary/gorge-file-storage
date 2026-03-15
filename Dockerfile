FROM golang:1.26-alpine3.22 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o gorge-file-storage ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /src/gorge-file-storage /usr/local/bin/gorge-file-storage

EXPOSE 8100

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8100/healthz || exit 1

ENTRYPOINT ["gorge-file-storage"]
