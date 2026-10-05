.PHONY: db image images run web test proto

# Start Postgres and apply migrations.
db:
	docker compose up -d --wait postgres
	docker compose run --rm migrate

# Build the machine image instances boot from. Rebuild after changing guestd.
image:
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

test:
	go test -race ./...

# Regenerate internal/guestapi/*.pb.go after editing guest.proto. Needs protoc,
# protoc-gen-go and protoc-gen-go-grpc on PATH.
proto:
	go generate ./internal/guestapi/
