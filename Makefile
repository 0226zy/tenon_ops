.PHONY: build build-remote web web-remote test run dev init-db clean

build: web
	go build -trimpath -ldflags '-s -w' -o bin/tenon-ops ./cmd/tenon-ops

web:
	cd web && npm ci --no-audit --no-fund && npm run build

web-remote:
	cd web && npm ci --no-audit --no-fund && VITE_BASE=/ops/ npm run build

# Builds a Linux binary whose SPA is mounted at /ops/ (nginx path deployment).
build-remote: web-remote
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o bin/tenon-ops-linux ./cmd/tenon-ops

test:
	go test ./...
	cd web && npm run build

run:
	go run ./cmd/tenon-ops serve -config deploy/config.local.yaml

dev:
	cd web && npm run dev

init-db:
	go run ./cmd/tenon-ops init-db -config deploy/config.local.yaml

clean:
	rm -rf bin internal/web/dist/assets
