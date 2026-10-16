module github.com/techpartners-asia/logger-go/adapters/otel

go 1.24

require (
	github.com/techpartners-asia/logger-go v0.1.0
	go.opentelemetry.io/otel/trace v1.35.0
)

require go.opentelemetry.io/otel v1.35.0 // indirect

replace github.com/techpartners-asia/logger-go => ../..
