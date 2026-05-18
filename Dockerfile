# syntax=docker/dockerfile:1.7
#
# Three-stage build for the ewws-platform-ui binary.
#
# Note on UID: distroless's `nonroot` user is UID 65532. Our wrapper Helm
# chart historically pins `runAsUser: 1000` — override to 65532 in values
# before deploying, otherwise the kernel will reject the process.

ARG GO_VERSION=1.24
ARG TEMPL_VERSION=v0.3.943
ARG ALPINE_VERSION=3.20

# --- Stage 1: templ generate -------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS templ
ARG TEMPL_VERSION
WORKDIR /src
ENV GOFLAGS=-trimpath
RUN go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN templ generate ./...

# --- Stage 2: build -----------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY --from=templ /src /src
ENV CGO_ENABLED=0 GOOS=linux
RUN go build \
        -trimpath \
        -ldflags="-s -w -buildid=" \
        -o /out/server \
        ./cmd/server

# --- Stage 3: runtime ---------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/server /server
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/server"]
