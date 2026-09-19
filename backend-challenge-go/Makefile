.PHONY: help deps-up deps-down up down build run migrate-up migrate-down migrate-version \
	test test-integration test-race test-race-docker cover cover-domain vet fmt check bruno \
	run-multi stop-multi

GO ?= go
COVER_PACKAGES ?= ./internal/...
STEPS ?= 1

help: ## Lista os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

deps-up: ## Sobe apenas as dependências (Postgres, Keycloak, LocalStack)
	docker compose up -d postgres keycloak localstack

deps-down: ## Derruba as dependências preservando o volume
	docker compose stop postgres keycloak localstack

up: ## Sobe o ambiente completo, incluindo a API
	docker compose up --build

down: ## Derruba o ambiente e remove os volumes
	docker compose down -v

build: ## Compila o binário da API
	$(GO) build -o bin/api ./cmd/api

run: ## Executa a API local usando as variáveis do .env
	$(GO) run ./cmd/api

migrate-up: ## Aplica as migrations pendentes
	$(GO) run ./cmd/migrate -command up

migrate-down: ## Reverte a última migration (STEPS=0 reverte todas)
	$(GO) run ./cmd/migrate -command down -steps $(STEPS)

migrate-version: ## Mostra a versão aplicada do schema
	$(GO) run ./cmd/migrate -command version

test: ## Executa a suíte de testes rápidos
	$(GO) test ./...

test-integration: ## Executa os testes de integração (sobe PostgreSQL via testcontainers)
	$(GO) test -tags=integration -count=1 ./tests/...

test-race: ## Executa a suíte com o detector de corridas
	$(GO) test -race ./...

# O detector de corridas exige cgo. Em máquinas sem toolchain C (Windows sem
# MinGW, por exemplo), este alvo roda a mesma suíte dentro do container oficial.
test-race-docker: ## Executa -race dentro de um container Go
	docker run --rm -v "$(CURDIR)":/src -w /src -e CGO_ENABLED=1 golang:1.27 go test -race ./...

cover: ## Relatório de cobertura de todos os pacotes internos
	$(GO) test -coverprofile=coverage.out $(COVER_PACKAGES)
	$(GO) tool cover -func=coverage.out

cover-domain: ## Gate de 100% de cobertura no domínio (ADR-019)
	./scripts/check-domain-coverage.sh

vet: ## Análise estática
	$(GO) vet ./...

bruno: ## Executa a coleção Bruno contra o ambiente local
	cd bruno && npx --yes @usebruno/cli@latest run . --env Local

fmt: ## Formata o código
	gofmt -w .

check: fmt vet test-race ## Formatação, análise estática e testes com -race

# Três processos independentes com conexões e memória próprias, conforme ADR-020.
# Usado nos cenários de concorrência e recuperação da etapa 11.
run-multi: build ## Sobe três instâncias locais nas portas 8081, 8082 e 8083
	HTTP_PORT=8081 ./bin/api & echo $$! > .pid-8081
	HTTP_PORT=8082 ./bin/api & echo $$! > .pid-8082
	HTTP_PORT=8083 ./bin/api & echo $$! > .pid-8083

stop-multi: ## Encerra as instâncias iniciadas por run-multi
	-@for pid in .pid-8081 .pid-8082 .pid-8083; do \
		if [ -f $$pid ]; then kill $$(cat $$pid) 2>/dev/null; rm -f $$pid; fi; \
	done
