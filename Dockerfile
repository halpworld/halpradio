# Multi-stage build for minimal container image
FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o halpradio main.go

# Runtime image with mpv for audio streaming support
FROM alpine:3.19

RUN apk add --no-cache \
    mpv \
    ca-certificates \
    tzdata \
    alsa-utils \
    ncurses

COPY --from=builder /src/halpradio /usr/local/bin/halpradio

ENTRYPOINT ["halpradio"]
