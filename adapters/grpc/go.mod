module github.com/techpartners-asia/logger-go/adapters/grpc

go 1.24

require (
	github.com/techpartners-asia/logger-go v0.1.0
	google.golang.org/grpc v1.72.0
)

require (
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)

replace github.com/techpartners-asia/logger-go => ../..
