#
# RailPush control-plane image (API + dashboard).
#
# This builds:
# - /api (Go binary)
# - /dashboard (Vite static assets)
#
# Runtime serves the SPA from ./dashboard/dist (see api/main.go spaHandler).
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

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /opt/railpush
COPY --from=api-build /out/railpush-api /opt/railpush/railpush-api
COPY --from=dashboard-build /src/dashboard/dist /opt/railpush/dashboard/dist
EXPOSE 8080
ENTRYPOINT ["/opt/railpush/railpush-api"]
