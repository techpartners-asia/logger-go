MODULES := . ./adapters/fiber ./adapters/gin ./adapters/echo ./adapters/chi ./adapters/mux ./adapters/grpc ./adapters/resty ./adapters/gorm ./adapters/pgx ./adapters/otel

.PHONY: test lint bench examples tidy fmt

test:
	@for m in $(MODULES); do echo "==> $$m"; (cd $$m && go test -race -count=1 ./...) || exit 1; done

lint:
	@test -z "$$(gofmt -l . | grep -v '^vendor/')" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	@for m in $(MODULES); do echo "==> vet $$m"; (cd $$m && go vet ./...) || exit 1; done
	@if command -v golangci-lint >/dev/null 2>&1; then \
		for m in $(MODULES); do echo "==> golangci-lint $$m"; (cd $$m && golangci-lint run ./...) || exit 1; done; \
	else echo "golangci-lint not installed, skipping"; fi

bench:
	go test -run '^$$' -bench . -benchmem ./...

examples:
	go run ./examples/basic
	LOG_FORMAT=console LOG_LEVEL=trace go run ./examples/basic

tidy:
	@for m in $(MODULES); do (cd $$m && go mod tidy) || exit 1; done

fmt:
	gofmt -w .
