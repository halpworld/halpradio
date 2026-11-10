# Multi-stage build for a minimal container image.
#
# Base images are pinned by digest (Dependabot's "docker" ecosystem bumps them).
# The Go image must satisfy the `go` directive in go.mod; the official image sets
# GOTOOLCHAIN=local, so an older image fails instead of silently downloading.
FROM golang:1.26.10-alpine3.24@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS builder

ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/halpworld/halpradio/pkg/app.Version=${VERSION}" \
    -o /out/halpradio .

# Runtime image with mpv for audio streaming support.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

RUN apk add --no-cache \
    mpv \
    ca-certificates \
    tzdata \
    alsa-utils \
    ncurses \
 && adduser -D -u 10001 -G audio radio

COPY --from=builder /out/halpradio /usr/local/bin/halpradio

USER radio
WORKDIR /home/radio

ENTRYPOINT ["halpradio"]
