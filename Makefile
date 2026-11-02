.PHONY: test lint demo examples

RUN = go run ./plugin/scripts

test:
	test -z "$$(gofmt -l plugin tests examples)"
	go vet ./...
	go test -count=1 ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

demo:
	$(RUN) --to 1.22 examples/muxcheck
	$(RUN) --to 1.27 examples/pinned

# Rebuilds the saved shop output. Real-project outputs in examples/real/
# need those projects cloned, see examples/README.md.
examples:
	$(RUN) --to 1.24 examples/shop > examples/shop-1.21-to-1.24.txt
