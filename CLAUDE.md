# ESPHome Config Project Guidelines

## Go Development Best Practices

### Code Quality and Idioms
- **Keep API surface minimal**: Only export what needs to be public. Prefer unexported fields and methods for internal implementation details.
- **Use proper error handling**: Always check errors from operations like `Write()`, `Read()`, etc. Don't ignore return values.
- **Follow concurrency patterns**: Use proper mutex patterns with `defer` for unlocking. Always handle race conditions in tests.
- **Implement interfaces properly**: Use interface-based design for testability and clean separation of concerns.
- **Use structured logging**: Prefer `slog` with proper attributes over format strings.

### Code Organization
- **Table-driven tests**: Use test case arrays for comprehensive testing scenarios.
- **Separate concerns**: Keep HTTP handlers, business logic, and persistence separate.
- **Package structure**: Organize code into logical packages with clear responsibilities.

### Build and Run Practices
- **NEVER commit binaries**: Always add build artifacts to `.gitignore`.
- **Use `go run` instead of `go build`**: `go run ./cmd/serve` preserves proper permissions and avoids binary creation.
- **Use `go fmt` over `gofmt`**: `go fmt` is the modern standard formatting tool.
- **Test with race detector**: Always run `go test -race` to catch concurrency issues.

### Memory and Performance
- **Minimize allocations**: Use appropriate buffer sizes, reuse objects where possible.
- **Handle resource cleanup**: Use `defer` for cleanup operations like closing files/connections.
- **Proper timeout handling**: Set reasonable timeouts for network operations.

### Error Patterns
- **Create custom error types**: Use proper error wrapping and unwrapping.
- **Graceful degradation**: Handle failures without crashing (e.g., persistence failures).
- **Appropriate logging levels**: Use debug/info/error levels appropriately.

## ESPHome Development

### Flash Memory Considerations
- **Measure impact**: Always compile and measure flash usage when adding new components.
- **Use `esp8266_disable_ssl_support: true`** for memory-constrained devices.
- **Keep registration payloads minimal**: Every byte counts on ESP8266.

### Service Registry Integration
- **Use reusable modules**: Include from `devices/common/` rather than duplicating code.
- **Configure reasonable intervals**: 30s heartbeat with 10min TTL is good default.
- **Provide both HTTP and UDP options**: HTTP for features (~23KB), UDP for minimal footprint.

## Git Practices
- **Commit incrementally**: Commit logical chunks of work, not everything at once.
- **Never commit binaries**: Use proper `.gitignore` patterns for build artifacts.
- **Use descriptive commit messages**: Follow conventional commit format with clear descriptions.
- **Fix history when needed**: Use `git filter-repo` to remove accidentally committed binaries.

## Code Review Standards
- **Address critical issues first**: Race conditions, memory leaks, security issues.
- **Test coverage**: Ensure comprehensive test coverage including edge cases.
- **Documentation**: Code should be self-documenting with clear naming and comments where needed.
- **Performance considerations**: Profile and measure rather than optimize prematurely.

## Deployment Guidelines
- **Health checks**: Always include `/health` endpoints for monitoring.
- **Metrics exposure**: Provide `/metrics` for operational visibility.
- **Graceful shutdown**: Handle SIGTERM/SIGINT properly with cleanup.
- **Configuration management**: Use command-line flags with sensible defaults.