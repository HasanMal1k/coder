.PHONY: db link image images run web test proto standin run-standin

# Start Postgres and apply migrations.
db:
	docker compose up -d --wait postgres
	docker compose run --rm migrate

# Build the link daemon, the Rust program on every machine (link/), as a
# small static binary, and leave it where the machine image picks it up.
# Needs Rust with the musl target for this computer's processor
# (`rustup target add $(LINK_TARGET)`) and protoc.
CARGO ?= $(shell command -v cargo 2>/dev/null || echo $(HOME)/.cargo/bin/cargo)
LINK_TARGET := $(shell uname -m)-unknown-linux-musl
link:
	cd link && $(CARGO) build --release --target $(LINK_TARGET)
	mkdir -p images/workspace/bin
	cp link/target/$(LINK_TARGET)/release/linkd images/workspace/bin/linkd

# Build the machine image instances boot from. Rebuild after changing guestd
# or the link daemon.
image: link
	docker build -f images/workspace/Dockerfile -t coder-workspace:dev .

# Build the image for every stack: the same machine with a toolchain already
# installed (Go, C++, .NET, or PostgreSQL and Redis). A machine can only be
# created on a stack whose image has been built.
STACKS := go cpp dotnet data
images: image
	for stack in $(STACKS); do \
		docker build -f images/workspace/Dockerfile --build-arg STACK=$$stack -t coder-workspace-$$stack:dev . || exit 1; \
	done

run:
	go run ./cmd/server

# The frontend dev server, on http://localhost:3000.
web:
	cd web && npm install && npm run dev

# The stand-in for the model is a module of its own, so it is tested apart,
# and the link daemon is Rust.
test:
	go test -race ./...
	cd tools/standin && go test -race ./...
	cd link && $(CARGO) test

# A program that answers in place of the model, so everything can be run
# without spending credits. See tools/standin/README.md.
standin:
	cd tools/standin && go run .

# The server pointed at the stand-in, never the real model: the environment
# wins over .env. tools/uicheck/build.mjs then checks a whole build.
run-standin:
	DEEPSEEK_API_KEY=standin LLM_BASE_URL=http://localhost:9099/v1 LLM_MODEL=standin go run ./cmd/server

# Regenerate the Go code of the two protocols after editing guest.proto or
# link.proto. Needs protoc, protoc-gen-go and protoc-gen-go-grpc on PATH. The
# link daemon generates its own Rust code from link.proto when it is built.
proto:
	go generate ./internal/guestapi/ ./internal/linkapi/
