FROM node:24-alpine3.24 AS frontend

WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY frontend ./frontend
COPY vite.config.mjs ./
RUN npm run build

FROM golang:1.27.1-alpine3.24 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/go-api ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/go-worker ./cmd/worker

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata libarchive-tools \
    && adduser -D -u 10001 app

WORKDIR /app
COPY --from=builder /out/go-api /app/go-api
COPY --from=builder /out/go-worker /app/go-worker
COPY --from=frontend /src/web/static/dist /app/static/dist
COPY web/static/style.css /app/static/style.css
RUN mkdir -p downloaded_images temp_downloads \
    && chown app:app downloaded_images temp_downloads

USER app
EXPOSE 5000

ENTRYPOINT ["/app/go-api"]
