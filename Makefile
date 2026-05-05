.PHONY: build dashboard server clean

# Build the dashboard and embed into Go binary
build: dashboard server

# Build the React dashboard
dashboard:
	cd dashboard && npm run build

# Copy dashboard build to embed location and build Go binary
server: dashboard
	rm -rf cmd/server/dashboard_dist
	cp -r dashboard/dist cmd/server/dashboard_dist
	go build -o bin/bastio ./cmd/server

# Development: build Go without dashboard
server-only:
	go build -o bin/bastio ./cmd/server

# Clean build artifacts
clean:
	rm -rf bin/ cmd/server/dashboard_dist dashboard/dist
