BINARY_NAME=sidekick
WINDOWS_BINARY=sidekick.exe
WINDOWS_PATH=/mnt/c/sidekick

all: build

build:
	@echo "[build] Building and installing sidekick..."
	@go install ./cmd/sidekick || (echo "[build] build failed"; exit 1)
	@echo "[build] ✓ sidekick installed to $(shell go env GOPATH)/bin/sidekick"
	@echo "[build] Run 'sidekick' from anywhere (ensure $(shell go env GOPATH)/bin is in PATH)"

build-local:
	@echo "[build-local] go build -o $(BINARY_NAME) ./cmd/sidekick"
	@go build -o $(BINARY_NAME) ./cmd/sidekick || (echo "[build-local] build failed"; exit 1)

build-windows:
	@echo "[build-windows] GOOS=windows GOARCH=amd64 go build -o $(WINDOWS_BINARY) ./cmd/sidekick"
	@GOOS=windows GOARCH=amd64 go build -o $(WINDOWS_BINARY) ./cmd/sidekick || (echo "[build-windows] build failed"; exit 1)
	@mkdir -p $(WINDOWS_PATH)
	@cp $(WINDOWS_BINARY) $(WINDOWS_PATH)/$(WINDOWS_BINARY)
	@echo "[build-windows] copied to $(WINDOWS_PATH)/$(WINDOWS_BINARY)"

serve: build-windows
	@echo "[serve] starting Windows server on 0.0.0.0:1337"
	@powershell.exe -Command "C:\\sidekick\\sidekick.exe --serve"

install:
	go install ./cmd/sidekick

run: build
	@echo "[run] sidekick"
	@sidekick

clean:
	@echo "[clean] rm -f $(BINARY_NAME) $(WINDOWS_BINARY)"
	@rm -f $(BINARY_NAME) $(WINDOWS_BINARY)
