# Stage 1: Build React SPA (always on native platform, never emulated)
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend
WORKDIR /app
COPY web/package*.json web/.npmrc ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go proxy (cross-compile on native platform via GOARCH)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /wg-sockd-ui ./cmd/wg-sockd-ui/

# Stage 3: Runtime — distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot
COPY --from=backend /wg-sockd-ui /wg-sockd-ui
COPY --from=frontend /app/dist /web/dist
EXPOSE 8080
ENTRYPOINT ["/wg-sockd-ui"]

