FROM golang:1.26.4

WORKDIR /src

COPY go.mod go.sum ./
COPY cmd/ ./cmd/

RUN CGO_ENABLED=0 go build -o /api ./cmd/api

USER 65532:65532

ENTRYPOINT ["/api"]
