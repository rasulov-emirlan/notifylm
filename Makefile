.PHONY: build run clean test test-integration lint deps

BINARY_NAME=notifylm
BUILD_DIR=./cmd/notifylm

build:
	go build -o $(BINARY_NAME) $(BUILD_DIR)

run: build
	./$(BINARY_NAME) -config config.yaml

run-debug: build
	./$(BINARY_NAME) -config config.yaml -debug

run-dry: build
	./$(BINARY_NAME) -config config.yaml -dry-run -debug

clean:
	rm -f $(BINARY_NAME)
	rm -rf data/

test:
	go test -v ./...

test-integration:
	OPENAI_API_KEY=$(OPENAI_API_KEY) go test -v -tags=integration -timeout 120s ./internal/classifier/

lint:
	golangci-lint run

deps:
	go mod tidy
	go mod download

# Setup creates necessary directories
setup:
	mkdir -p data/whatsapp data/telegram
	cp -n config.example.yaml config.yaml 2>/dev/null || true
	@echo "Edit config.yaml with your credentials"
