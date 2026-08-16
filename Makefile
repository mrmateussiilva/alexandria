APP_NAME := alexandria
BIN_DIR := bin

.PHONY: dev dev-backend dev-frontend test lint build clean

dev:
	$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	go run ./cmd/alexandria

dev-frontend:
	npm --prefix web run dev

test:
	go test ./...

lint:
	go vet ./...
	npm --prefix web run lint

build:
	npm --prefix web run build
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP_NAME) ./cmd/alexandria

clean:
	rm -rf $(BIN_DIR) web/dist
