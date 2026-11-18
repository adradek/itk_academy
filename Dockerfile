FROM golang:1.26.4
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN CGO_ENABLED=0 go build -o /api ./cmd/api

RUN mkdir -p /tmp/go-mod /tmp/go-build \
    && chown 65532:65532 /tmp/go-mod /tmp/go-build

USER 65532:65532

ENTRYPOINT ["/api"]
