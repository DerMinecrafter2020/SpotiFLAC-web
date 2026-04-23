# syntax=docker/dockerfile:1.7

FROM node:22-bookworm-slim AS frontend-builder
WORKDIR /src/frontend

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@10.0.0 --activate && pnpm install --frozen-lockfile --ignore-scripts

COPY frontend/ ./
COPY wails.json /src/wails.json
RUN pnpm run postinstall
RUN pnpm build

FROM golang:1.26-bookworm AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags web -o /out/spotiflac-web .

FROM debian:bookworm-slim
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /out/spotiflac-web /usr/local/bin/spotiflac-web
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist

ENV PORT=8080
ENV SPOTIFLAC_APP_DIR=/app/data
ENV SPOTIFLAC_MUSIC_DIR=/music

RUN mkdir -p /app/data /music

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/spotiflac-web"]
