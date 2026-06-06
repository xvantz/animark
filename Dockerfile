# ── Stage 1: Build ──
FROM golang:1.25-alpine AS builder
WORKDIR /build
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(git describe --tags --always || echo dev)" -o /animark ./cmd/animark

# ── Stage 2: Runtime ──
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git openssh-client
RUN adduser -D -h /data animark
USER animark
WORKDIR /data
COPY --from=builder /animark /usr/local/bin/animark
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["animark"]
CMD ["-addr", ":8080", "-data", "/data"]
