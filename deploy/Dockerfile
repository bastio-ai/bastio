# Stage 1: Build dashboard
FROM node:22-alpine AS dashboard
WORKDIR /app/dashboard
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ .
RUN npm run build

# Stage 2: Build Go binary with embedded dashboard
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=dashboard /app/dashboard/dist ./cmd/server/dashboard_dist
RUN CGO_ENABLED=0 go build -o /bastio ./cmd/server

# Stage 3: Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /bastio /usr/local/bin/bastio

EXPOSE 4000
ENTRYPOINT ["bastio"]
