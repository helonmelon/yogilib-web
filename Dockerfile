# --- Build stage ---
FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o yogilib .

# --- Run stage ---
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/yogilib ./yogilib
COPY --from=builder /app/static ./static
COPY --from=builder /app/templates ./templates

# DB lives on a mounted volume at /data so it survives redeploys
ENV PORT=8080
ENV DB_PATH=/data/yogilib.db

EXPOSE 8080

CMD ["./yogilib"]
