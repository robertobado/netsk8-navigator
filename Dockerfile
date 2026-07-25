# syntax=docker/dockerfile:1

# --- frontend build: produces backend/internal/web/dist, embedded below ---
FROM node:22-alpine AS frontend
RUN corepack enable
WORKDIR /src
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/
RUN cd frontend && pnpm install --frozen-lockfile
COPY frontend frontend/
COPY backend/internal/web backend/internal/web
RUN cd frontend && pnpm build

# --- backend build: a single static binary with the SPA embedded ---
FROM golang:1.26-alpine AS backend
ARG VERSION=dev
WORKDIR /src
COPY backend/go.mod backend/go.sum backend/
RUN cd backend && go mod download
COPY backend backend/
COPY --from=frontend /src/backend/internal/web/dist backend/internal/web/dist
RUN cd backend && CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /out/netsk8-navigator .

# --- final image ---
FROM gcr.io/distroless/static-debian12
COPY --from=backend /out/netsk8-navigator /netsk8-navigator
# Distroless has no shell; the kubeconfig is mounted read-only at run time
# (see README) — the binary itself never needs write access to it.
EXPOSE 8080
# 0.0.0.0 so Docker's port publishing can reach it (loopback-only, main.go's
# default, isn't reachable from outside the container's network namespace).
# Keep the port bound to the host's loopback when you run this image — see
# the README's security model, which applies identically in a container.
ENV ADDR=0.0.0.0:8080
ENTRYPOINT ["/netsk8-navigator"]
