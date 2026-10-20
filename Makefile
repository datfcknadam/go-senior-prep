.PHONY: panel test test-one race check bench vet

panel:
	go run ./cmd/panel

test:
	go test ./...

test-one:
	go test -race -v $(T)

race:
	go test -race ./...

check:
	go test -race -tags solution ./...

bench:
	go test -run '^$$' -bench . -benchmem -tags solution ./...

vet:
	go vet ./...
	go vet -tags solution ./...
