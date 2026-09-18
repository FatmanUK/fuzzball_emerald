# Fuzzball Emerald
#
# Quick start:
#   make certs db-up import run      run on the host
#   make pod-build pod-run           run in a container
#
# Override any variable on the command line, e.g.
#   make pod-run LINE_PORT=5202

BINARY      := fbemerald
IMAGE       ?= localhost/fbemerald
TAG         ?= dev
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# Where the TLS material lives. There is no cleartext listener, so the server
# will not start without it.
TLS_DIR     ?= deploy/tls
CERT_FILE   := $(TLS_DIR)/cert.pem
KEY_FILE    := $(TLS_DIR)/key.pem
CERT_CN     ?= localhost
CERT_DAYS   ?= 365

# Listener ports on the host.
LINE_PORT   ?= 4202
WSS_PORT    ?= 4203

# Postgres, run as a container for local work.
DB_NAME     ?= fbemerald
# Tests get a database of their own. They isolate themselves into a scratch
# schema as well, but a world you have imported is not something to risk on
# that working: keeping them apart means a bug in the isolation costs nothing.
TEST_DB_NAME?= fbemerald_test
DB_USER     ?= fbemerald
DB_PASSWORD ?= fbemerald
DB_PORT     ?= 55432
DB_IMAGE    ?= docker.io/library/postgres:17-alpine
PG_CONTAINER:= fbemerald-pg
NETWORK     ?= fbemerald

# From inside a container, Postgres is reached by its container name; from the
# host, by the published port.
DB_URL      ?= postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable
DB_URL_POD  := postgres://$(DB_USER):$(DB_PASSWORD)@$(PG_CONTAINER):5432/$(DB_NAME)?sslmode=disable
TEST_DB_URL := postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DB_PORT)/$(TEST_DB_NAME)?sslmode=disable

APP_CONTAINER := fbemerald

# The distroless base runs as the unprivileged "nonroot" user. Rootless Podman
# maps that to a subordinate uid on the host, which cannot read a private key
# owned by you with the usual 0600 permissions. Mapping your uid onto it makes
# the container process *be* you, so the key stays 0600 and still opens.
NONROOT_UID := 65532
USERNS      ?= keep-id:uid=$(NONROOT_UID),gid=$(NONROOT_UID)

# A world to import when none is given. The muf/ sources sit beside the dump,
# so a container gets the whole directory mounted rather than the one file.
DUMP        ?= testdata/starterdb/starterdb.db
DUMP_DIR     = $(patsubst %/,%,$(dir $(abspath $(DUMP))))
DUMP_BASE    = $(notdir $(DUMP))

GO          ?= go
GOFLAGS     ?=
LDFLAGS     := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo "Fuzzball Emerald"
	@echo
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / \
		{printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "  Database: $(DB_URL)"
	@echo "  Tests:    $(TEST_DB_URL)"
	@echo "  TLS:      $(TLS_DIR)"

# --- building ---------------------------------------------------------------

.PHONY: build
build: ## Build the server binary
	$(GO) build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/fbemerald

.PHONY: generate
generate: ## Regenerate the @tune table from the Fuzzball sources
	$(GO) generate ./...

.PHONY: clean
clean: ## Remove build artefacts
	rm -f $(BINARY) coverage.out coverage.html
	rm -rf build

.PHONY: distclean
distclean: clean pod-clean ## Remove artefacts and containers
	@echo "$(TLS_DIR) was left alone; remove it by hand if you mean to."

# --- testing ----------------------------------------------------------------

# The store tests need a database and skip without one. They are pointed at a
# database of their own, never the one holding your world.
.PHONY: test
test: ## Run the tests (starts Postgres if it is not already up)
	@$(MAKE) --no-print-directory db-up
	FBE_TEST_DATABASE_URL="$(TEST_DB_URL)" $(GO) test -race ./...

.PHONY: test-short
test-short: ## Run only the tests that need no database
	$(GO) test ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: check
check: vet test ## Vet and test

.PHONY: cover
cover: ## Write an HTML coverage report
	@$(MAKE) --no-print-directory db-up
	FBE_TEST_DATABASE_URL="$(TEST_DB_URL)" $(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

# --- TLS --------------------------------------------------------------------

.PHONY: certs
certs: $(KEY_FILE) ## Generate a self-signed certificate for local use

$(KEY_FILE):
	@mkdir -p $(TLS_DIR)
	openssl req -x509 -newkey rsa:4096 -nodes -days $(CERT_DAYS) \
		-subj "/CN=$(CERT_CN)" \
		-addext "subjectAltName=DNS:$(CERT_CN),DNS:localhost,IP:127.0.0.1" \
		-keyout $(KEY_FILE) -out $(CERT_FILE)
	@chmod 600 $(KEY_FILE)
	@echo "wrote $(CERT_FILE) and $(KEY_FILE) (self-signed, for local use only)"

# --- database ---------------------------------------------------------------

.PHONY: db-up
db-up: ## Start Postgres and wait for it
	@if [ -z "$$(podman ps -q -f name=^$(PG_CONTAINER)$$)" ]; then \
		podman network exists $(NETWORK) || podman network create $(NETWORK) >/dev/null; \
		podman rm -f $(PG_CONTAINER) >/dev/null 2>&1 || true; \
		echo "starting Postgres..."; \
		podman run -d --name $(PG_CONTAINER) --network $(NETWORK) \
			-e POSTGRES_USER=$(DB_USER) \
			-e POSTGRES_PASSWORD=$(DB_PASSWORD) \
			-e POSTGRES_DB=$(DB_NAME) \
			-p $(DB_PORT):5432 \
			-v fbemerald-pgdata:/var/lib/postgresql/data \
			$(DB_IMAGE) >/dev/null; \
		for i in $$(seq 1 60); do \
			podman exec $(PG_CONTAINER) pg_isready -U $(DB_USER) >/dev/null 2>&1 && break; \
			sleep 1; \
		done; \
		podman exec $(PG_CONTAINER) pg_isready -U $(DB_USER) >/dev/null 2>&1 \
			|| { echo "Postgres did not become ready"; exit 1; }; \
		echo "Postgres is ready on port $(DB_PORT)"; \
	fi
	@podman exec $(PG_CONTAINER) psql -U $(DB_USER) -d postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname='$(TEST_DB_NAME)'" | grep -q 1 \
		|| podman exec $(PG_CONTAINER) createdb -U $(DB_USER) $(TEST_DB_NAME)

.PHONY: db-down
db-down: ## Stop Postgres, keeping its data
	-podman rm -f $(PG_CONTAINER) 2>/dev/null

.PHONY: db-reset
db-reset: ## Destroy the database and its stored world
	-podman rm -f $(PG_CONTAINER) 2>/dev/null
	-podman volume rm fbemerald-pgdata 2>/dev/null
	@echo "database destroyed; run 'make import' to load a world again"

.PHONY: psql
psql: db-up ## Open a psql shell
	podman exec -it $(PG_CONTAINER) psql -U $(DB_USER) -d $(DB_NAME)

# --- running on the host ----------------------------------------------------

.PHONY: import
import: build db-up ## Import a legacy world (override with DUMP=path/to.db)
	FBE_DATABASE_URL="$(DB_URL)" ./$(BINARY) import -force $(DUMP)

.PHONY: run
run: build certs db-up ## Run the server on the host
	FBE_DATABASE_URL="$(DB_URL)" \
	FBE_TLS_CERT_FILE=$(CERT_FILE) \
	FBE_TLS_KEY_FILE=$(KEY_FILE) \
	FBE_LINE_ADDR=":$(LINE_PORT)" \
	FBE_WSS_ADDR=":$(WSS_PORT)" \
	./$(BINARY) serve

.PHONY: connect
connect: ## Connect to a running server with openssl
	@echo "connecting to localhost:$(LINE_PORT) - type 'connect <name> <password>'"
	@openssl s_client -quiet -connect localhost:$(LINE_PORT)

# --- the golden-output oracle -----------------------------------------------

# Fuzzball 7 built from the C sources on the mother branch. The golden tests
# run the same session against it and against this server, and diff the two.
ORACLE_IMAGE ?= localhost/fbmuck-oracle
ORACLE_REF   ?= origin/mother

.PHONY: golden-build
golden-build: ## Build the C Fuzzball the golden tests compare against
	@echo "extracting $(ORACLE_REF)..."
	@rm -rf build/oracle && mkdir -p build/oracle
	@git archive $(ORACLE_REF) | tar -x -C build/oracle
	@cp deploy/golden/Containerfile.fbmuck build/oracle/
	podman build -t $(ORACLE_IMAGE) -f build/oracle/Containerfile.fbmuck build/oracle
	@rm -rf build/oracle

.PHONY: golden
golden: ## Run the differential tests against the C server
	@podman image exists $(ORACLE_IMAGE) \
		|| { echo "the oracle is not built; run 'make golden-build'"; exit 1; }
	$(GO) test -v -count=1 ./internal/golden/

.PHONY: golden-clean
golden-clean: ## Remove the oracle image
	-podman rmi -f $(ORACLE_IMAGE) 2>/dev/null

# --- running in a container -------------------------------------------------

.PHONY: pod-build
pod-build: ## Build the container image
	podman build --build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(TAG) -f deploy/Containerfile .

.PHONY: pod-run
pod-run: pod-build certs db-up ## Run the server in a container
	@podman network exists $(NETWORK) || podman network create $(NETWORK) >/dev/null
	@podman rm -f $(APP_CONTAINER) >/dev/null 2>&1 || true
	podman run -d --name $(APP_CONTAINER) \
		--network $(NETWORK) \
		--userns=$(USERNS) \
		-p $(LINE_PORT):4202 \
		-p $(WSS_PORT):4203 \
		-e FBE_DATABASE_URL="$(DB_URL_POD)" \
		-e FBE_LOG_FORMAT=json \
		-v ./$(TLS_DIR):/etc/fbemerald/tls:ro,z \
		$(IMAGE):$(TAG) serve
	@echo "waiting for the server..."
	@for i in $$(seq 1 30); do \
		podman logs $(APP_CONTAINER) 2>&1 | grep -q 'listening' && break; \
		podman ps -q -f name=^$(APP_CONTAINER)$$ | grep -q . \
			|| { echo "the container exited:"; podman logs $(APP_CONTAINER); exit 1; }; \
		sleep 1; \
	done
	@podman logs $(APP_CONTAINER) 2>&1 | tail -5
	@echo
	@echo "listening on $(LINE_PORT) (TLS) and $(WSS_PORT) (WebSocket)"
	@echo "connect with: make connect"

.PHONY: pod-import
pod-import: pod-build db-up ## Import a world using the container image
	podman run --rm --network $(NETWORK) \
		--userns=$(USERNS) \
		-e FBE_DATABASE_URL="$(DB_URL_POD)" \
		-v "$(DUMP_DIR)":/dump:ro,z \
		$(IMAGE):$(TAG) import -force /dump/$(DUMP_BASE)

.PHONY: pod-logs
pod-logs: ## Follow the container's logs
	podman logs -f $(APP_CONTAINER)

.PHONY: pod-stop
pod-stop: ## Stop the server container
	-podman rm -f $(APP_CONTAINER) 2>/dev/null

.PHONY: pod-clean
pod-clean: pod-stop db-down ## Remove containers and the image
	-podman rmi -f $(IMAGE):$(TAG) 2>/dev/null
	-podman network rm $(NETWORK) 2>/dev/null
