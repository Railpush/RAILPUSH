#
# RailPush control-plane image (API + dashboard + CLI).
#
# This builds:
# - /api (Go binary)
# - /cli (Go binary — cross-compiled for linux/darwin/windows)
# - /dashboard (Vite static assets)
#
# Runtime serves the SPA from ./dashboard/dist (see api/main.go spaHandler).
# CLI binaries are served at /dl/railpush-{os}-{arch} for user download.
#

FROM node:20-alpine AS dashboard-build
WORKDIR /src/dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ ./
RUN npm run build

FROM golang:1.24-alpine AS api-build
WORKDIR /src/api
RUN apk add --no-cache git ca-certificates
COPY api/go.mod api/go.sum ./
RUN go mod download
COPY api/ ./
RUN CGO_ENABLED=0 go build -o /out/railpush-api .

FROM golang:1.24-alpine AS cli-build
WORKDIR /src/cli
COPY cli/go.mod ./
RUN go mod download
COPY cli/ ./
RUN CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o /out/railpush-linux-amd64   . && \
    CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o /out/railpush-linux-arm64   . && \
    CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -o /out/railpush-darwin-amd64  . && \
    CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o /out/railpush-darwin-arm64  . && \
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /out/railpush-windows-amd64.exe .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /opt/railpush
COPY --from=api-build /out/railpush-api /opt/railpush/railpush-api
COPY --from=cli-build /out/railpush-* /opt/railpush/cli/
COPY --from=dashboard-build /src/dashboard/dist /opt/railpush/dashboard/dist
EXPOSE 8080
ENTRYPOINT ["/opt/railpush/railpush-api"]
