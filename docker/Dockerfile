FROM golang:1.23 AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./

RUN go mod download

COPY ./cmd/proxy cmd/proxy
COPY ./pkg/ pkg/


RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -a -o elika-proxy-srv cmd/proxy/main.go

# Use alpine as minimal base image to package the manager binary
FROM alpine:latest AS final
WORKDIR /
COPY --from=builder /workspace/elika-proxy-srv /elika-proxy-srv
COPY ./docker/entrypoint.sh  /entrypoint.sh

# Ensure the script has Unix-style line endings and executable permissions
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
