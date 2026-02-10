FROM golang:1.25 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/web ./cmd/web

FROM alpine:3.22

WORKDIR /app

RUN adduser -D -H -u 10001 appuser

COPY --from=builder /out/web /app/web
RUN mkdir -p /app/data/uploads && chown -R appuser:appuser /app

USER appuser

EXPOSE 8080

ENTRYPOINT ["/app/web"]
