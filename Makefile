BINARY_NAME=sidekick
WINDOWS_BINARY=sidekick.exe
WINDOWS_PATH=/mnt/c/sidekick

all: build

build:
	@echo "[build] go build -o $(BINARY_NAME) ./cmd/sidekick"
	@go build -o $(BINARY_NAME) ./cmd/sidekick || (echo "[build] build failed"; exit 1)

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

run:
	@echo "[run] go build -o $(BINARY_NAME) ./cmd/sidekick"
	@go build -o $(BINARY_NAME) ./cmd/sidekick || (echo "[run] build failed"; exit 1)
	@echo "[run] ./$(BINARY_NAME)"
	@./$(BINARY_NAME)

clean:
	@echo "[clean] rm -f $(BINARY_NAME) $(WINDOWS_BINARY)"
	@rm -f $(BINARY_NAME) $(WINDOWS_BINARY)
