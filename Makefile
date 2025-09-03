
BINARY_NAME=sidekick
CMD_DIR=./cmd/sidekick

.PHONY: all build install run clean tidy

all: build

build:
	go build -o $(BINARY_NAME) $(CMD_DIR)

install:
	go install $(CMD_DIR)

run: build
	./$(BINARY_NAME) "Tell me a joke about ducks"

clean:
	rm -f $(BINARY_NAME)

tidy:
	go mod tidy
