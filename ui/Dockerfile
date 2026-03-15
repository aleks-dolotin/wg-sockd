# Stage 1: Build React SPA
FROM node:20-alpine AS frontend
WORKDIR /app
COPY web/package*.json web/.npmrc ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go proxy
FROM golang:1.22-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /wg-sockd-ui ./cmd/wg-sockd-ui/

# Stage 3: Runtime — distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot
COPY --from=backend /wg-sockd-ui /wg-sockd-ui
COPY --from=frontend /app/dist /web/dist
EXPOSE 8080
ENTRYPOINT ["/wg-sockd-ui"]

