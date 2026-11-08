.PHONY: check fmt vet lint test race integration bench cover tidy

PKGS := ./...

## check: everything CI runs. Must pass before a feature is done.
check: fmt vet lint race

## fmt: format code (gofumpt + goimports via golangci-lint)
fmt:
	golangci-lint fmt

## vet: go vet
vet:
	go vet $(PKGS)

## lint: golangci-lint (config in .golangci.yml)
lint:
	golangci-lint run

## test: unit tests
test:
	go test $(PKGS)

## race: unit tests with the race detector
race:
	go test -race -count=1 $(PKGS)

## integration: tests that need Docker (Postgres, MySQL, MongoDB)
integration:
	go test -race -count=1 -tags=integration $(PKGS)

## bench: benchmarks with allocation stats
bench:
	go test -run=^$$ -bench=. -benchmem $(PKGS)

## cover: coverage report in coverage.html
cover:
	go test -coverprofile=coverage.out $(PKGS)
	go tool cover -html=coverage.out -o coverage.html

## tidy: tidy go.mod
tidy:
	go mod tidy
