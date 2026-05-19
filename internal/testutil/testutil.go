// Package testutil provides shared test utilities for lite-mail.
//
// This package contains helpers for:
//
// # Test Fixtures
//
// The fixture functions load sample MIME messages from the testdata directory
// for use in unit and integration tests. Files are resolved relative to
// the calling test file.
//
//	db := SetupTestDB(t)
//	defer TeardownTestDB(t, db)
//
// Example:
//
//	 data, err := LoadFixture("simple-text.eml")
//	 if err != nil {
//	     t.Fatal(err)
//	 }
//
// # Database Setup
//
// SetupTestDB and TeardownTestDB handle test database lifecycle.
// They use TEST_DATABASE_URL environment variable and skip tests if not set.
//
// # HTTP Test Server
//
// NewTestServer creates an httptest.Server for testing HTTP handlers.
// PSKTransport and NewPSKClient assist with authenticated requests.
//
// # Constants
//
// DefaultPSK is a test PSK value for use in tests. It should be replaced
// with environment-based configuration in production.
package testutil

// DefaultPSK is a test PSK for use in unit tests.
const DefaultPSK = "test-psk-for-unit-tests"

// MaxMessageBytes is the default max message size (25 MiB).
const MaxMessageBytes = 25 * 1024 * 1024
