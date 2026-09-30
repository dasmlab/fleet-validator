FROM docker.io/library/golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o fleet-validator ./cmd/fleet-validator

FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source="https://github.com/dasmlab/fleet-validator" \
      org.opencontainers.image.description="ACM hub and managed cluster readiness validator" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=builder /workspace/fleet-validator /fleet-validator
USER 65532:65532
EXPOSE 8090
ENTRYPOINT ["/fleet-validator"]
