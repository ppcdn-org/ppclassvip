APP := ppclassvip
CONFIG ?= configs/config.yaml
EMAIL ?= teacher@example.com
PASSWORD ?= change-me-123
NICKNAME ?= Teacher
ROLE ?= teacher

.PHONY: run build agent agent-setup test fmt vet tidy seed db-up db-down

run:
	go run ./cmd/server -config $(CONFIG)

build:
	go build -trimpath -o bin/$(APP) ./cmd/server

agent:
	go build -trimpath -ldflags "-s -w -H=windowsgui" -o bin/ppclass-agent.exe ./cmd/agent

agent-setup: agent
	go build -trimpath -ldflags "-s -w -H=windowsgui" -o bin/ppclass-agent-setup.exe ./cmd/agent-installer

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

seed:
	go run ./cmd/seed -config $(CONFIG) -email $(EMAIL) -password "$(PASSWORD)" -nickname "$(NICKNAME)" -role $(ROLE)

db-up:
	docker compose up -d mysql

db-down:
	docker compose down
