.PHONY: build web test run dev init-db clean

build: web
	go build -trimpath -ldflags '-s -w' -o bin/tenon-ops ./cmd/tenon-ops

web:
	cd web && npm ci --no-audit --no-fund && npm run build

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
