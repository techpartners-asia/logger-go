module github.com/techpartners-asia/logger-go/adapters/resty

go 1.24

require (
	github.com/techpartners-asia/logger-go v0.1.0
	resty.dev/v3 v3.0.0-rc.4
)

require golang.org/x/net v0.43.0 // indirect

replace github.com/techpartners-asia/logger-go => ../..
