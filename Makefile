BINARY_NAME=sidekick

all: build

build:
	go build -o $(BINARY_NAME) ./cmd/sidekick

install:
	go install ./cmd/sidekick

run:
	go run ./cmd/sidekick

clean:
	rm -f $(BINARY_NAME)
