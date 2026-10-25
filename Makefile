UV ?= uv
GO ?= go
PNPM ?= pnpm

.PHONY: sync format lint typecheck test test-python test-go test-typescript test-c check

sync:
	$(UV) sync --directory python --all-extras --locked

format:
	$(UV) run --directory python --all-extras --locked ruff check --select I --fix src tests
	$(UV) run --directory python --all-extras --locked ruff format src tests

lint:
	$(UV) run --directory python --all-extras --locked ruff check src tests
	$(UV) run --directory python --all-extras --locked ruff format --check src tests

typecheck:
	$(UV) run --directory python --all-extras --locked ty check

test: test-python test-go test-typescript

test-python:
	$(UV) run --directory python --all-extras --locked pytest -q

test-go:
	$(GO) -C go test ./...
	$(GO) -C go vet ./...

test-typescript:
	$(PNPM) check

# Configure c/build first; see c/README.md for native dependency requirements.
test-c:
	cmake --build c/build --parallel 4
	ctest --test-dir c/build --output-on-failure --parallel 4

check: lint typecheck test
