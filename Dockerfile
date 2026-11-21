FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/rag-index ./cmd/rag-index \
    && CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/server /app/server
COPY --from=builder /out/rag-index /app/rag-index
COPY --from=builder /out/seed /app/seed
COPY knowledge /app/knowledge

USER app

EXPOSE 8080

ENTRYPOINT ["/app/server"]
