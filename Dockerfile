# syntax=docker/dockerfile:1.7

FROM alpine:3.22 AS frontend-builder
WORKDIR /src/frontend

RUN apk add --no-cache nodejs npm

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN npm install -g pnpm@10.0.0 && pnpm install --frozen-lockfile --ignore-scripts

COPY frontend/ ./
COPY wails.json /src/wails.json
RUN pnpm run postinstall
RUN pnpm build

FROM golang:1.26-alpine AS go-builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags web -o /out/spotiflac-web .

FROM alpine:3.22
WORKDIR /app

RUN apk add --no-cache ffmpeg ca-certificates tzdata

COPY --from=go-builder /out/spotiflac-web /usr/local/bin/spotiflac-web
COPY --from=frontend-builder /src/frontend/dist ./frontend/dist

ENV PORT=8080
ENV SPOTIFLAC_APP_DIR=/app/data
ENV SPOTIFLAC_MUSIC_DIR=/music

RUN mkdir -p /app/data /music

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/spotiflac-web"]
