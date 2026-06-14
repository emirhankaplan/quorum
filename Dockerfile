# syntax=docker/dockerfile:1
# Multi-stage build: compile the React UI, compile the Go cluster binary, then
# ship a tiny runtime image that serves both — the whole demo in one container.

# --- stage 1: build the frontend ---
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- stage 2: build the backend ---
FROM golang:1.22-alpine AS server
WORKDIR /src/cluster
COPY cluster/go.mod cluster/go.sum ./
RUN go mod download
COPY cluster/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /quorum ./cmd/quorum

# --- stage 3: runtime ---
FROM alpine:3.20
RUN adduser -D -u 10001 quorum
WORKDIR /app
COPY --from=server /quorum /app/quorum
COPY --from=web /web/dist /app/web/dist
USER quorum
EXPOSE 8080
ENTRYPOINT ["/app/quorum", "-addr", ":8080", "-static", "/app/web/dist"]
