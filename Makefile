BINARY_NAME=sidekick

all: build

build:
	@echo "[build] go build -o $(BINARY_NAME) ./cmd/sidekick"
	@go build -o $(BINARY_NAME) ./cmd/sidekick || (echo "[build] build failed"; exit 1)

install:
	go install ./cmd/sidekick

run:
	@echo "[run] go build -o $(BINARY_NAME) ./cmd/sidekick"
	@go build -o $(BINARY_NAME) ./cmd/sidekick || (echo "[run] build failed"; exit 1)
	@echo "[run] ./$(BINARY_NAME)"
	@./$(BINARY_NAME)

clean:
	@echo "[clean] rm -f $(BINARY_NAME)"
	@rm -f $(BINARY_NAME)
