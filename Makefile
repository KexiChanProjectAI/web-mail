.PHONY: all build run test test-integration test-worker vet clean db-setup migrate help

# Default target
all: vet build test

# Build the server binary
build:
	go build -o bin/lite-mail ./cmd/server

# Run the server
run:
	go run ./cmd/server

# Run all tests
test:
	go test ./...

# Run integration tests (requires TEST_DATABASE_URL)
test-integration:
	TEST_DATABASE_URL=$(TEST_DATABASE_URL) go test ./tests/integration/ -v

# Run worker tests
test-worker:
	cd worker && npm test

# Run go vet for static analysis
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Database setup instructions
db-setup:
	@echo "=== Database Setup ==="
	@echo "1. Connect to MariaDB as root:"
	@echo "   mysql -u root -p"
	@echo ""
	@echo "2. Create the database:"
	@echo "   CREATE DATABASE lite_mail CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
	@echo ""
	@echo "3. Create a user:"
	@echo "   CREATE USER 'lite_mail'@'localhost' IDENTIFIED BY 'your_password';"
	@echo "   GRANT ALL PRIVILEGES ON lite_mail.* TO 'lite_mail'@'localhost';"
	@echo "   FLUSH PRIVILEGES;"
	@echo ""
	@echo "4. Update .env with your DATABASE_URL:"
	@echo "   DATABASE_URL=mysql://lite_mail:your_password@localhost:3306/lite_mail"
	@echo ""
	@echo "Migrations run automatically on first server start."

# Run migrations
migrate:
	@echo "Migrations run automatically on server start."
	@echo "To run migrations manually (if supported):"
	@echo "  go run ./cmd/server -migrate-only"

# Show help
help:
	@echo "=== lite-mail Makefile ==="
	@echo ""
	@echo "Targets:"
	@echo "  all              Run vet, build, and test (default)"
	@echo "  build            Build the server binary to bin/lite-mail"
	@echo "  run              Run the server"
	@echo "  test             Run all Go tests"
	@echo "  test-integration Run integration tests (set TEST_DATABASE_URL)"
	@echo "  test-worker      Run worker tests"
	@echo "  vet              Run go vet for static analysis"
	@echo "  clean            Remove build artifacts"
	@echo "  db-setup         Show database setup instructions"
	@echo "  migrate          Migration instructions"
	@echo "  help             Show this help message"
