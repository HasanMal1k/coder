.PHONY: db image run test proto

# Start Postgres and apply migrations.
db:
	docker compose up -d --wait postgres
	docker compose run --rm migrate

# Build the machine image instances boot from. Rebuild after changing guestd.
image:
	docker build -f images/workspace/Dockerfile -t coder-workspace:dev .

run:
	go run ./cmd/server

test:
	go test -race ./...

# Regenerate internal/guestapi/*.pb.go after editing guest.proto. Needs protoc,
# protoc-gen-go and protoc-gen-go-grpc on PATH.
proto:
	go generate ./internal/guestapi/
