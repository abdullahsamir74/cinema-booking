# syntax=docker/dockerfile:1

# ---------- build ----------
FROM golang:1.27-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/cinema \
    ./cmd/server

# ---------- runtime ----------
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -H -u 10001 appuser

COPY --from=build /out/cinema /usr/local/bin/cinema

USER appuser
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/cinema"]
