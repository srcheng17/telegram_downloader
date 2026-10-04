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
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/adminctl ./cmd/adminctl
RUN cd tools/tdl-auth-helper && CGO_ENABLED=0 GOOS=linux go build -mod=readonly -p=1 -trimpath -ldflags='-s -w' -o /out/tdl-auth-helper .
RUN cd tools/tdl-auth-helper && CGO_ENABLED=0 GOOS=linux go build -mod=readonly -p=1 -trimpath -ldflags='-s -w -X github.com/iyear/tdl/pkg/consts.Version=0.20.4 -X github.com/iyear/tdl/pkg/consts.Commit=9d7d49e -X github.com/iyear/tdl/pkg/consts.CommitDate=2026-08-23T17:15:46Z' -o /out/tdl github.com/iyear/tdl

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata libarchive-tools \
    && adduser -D -u 10001 app

WORKDIR /app
COPY --from=builder /out/go-api /app/go-api
COPY --from=builder /out/go-worker /app/go-worker
COPY --from=builder /out/adminctl /app/adminctl
COPY --from=builder /out/tdl-auth-helper /app/tdl-auth-helper
COPY --from=builder /out/tdl /app/tdl
COPY --from=frontend /src/web/static/dist /app/static/dist
COPY web/static/style.css /app/static/style.css
COPY web/static/ocr /app/static/ocr
RUN mkdir -p downloaded_images temp_downloads telegram-private source-retention komga-edit-backups \
    && chmod 0700 telegram-private source-retention komga-edit-backups \
    && chown app:app downloaded_images temp_downloads telegram-private source-retention komga-edit-backups

USER app
EXPOSE 5000

ENTRYPOINT ["/app/go-api"]
