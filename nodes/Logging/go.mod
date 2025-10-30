module github.com/ayden-boyko/Piranid/nodes/Logging

go 1.24.0

toolchain go1.24.9

require (
	Piranid/pkg v0.0.0
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/influxdata/influxdb-client-go/v2 v2.14.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/influxdata/line-protocol v0.0.0-20200327222509-2487e7298839 // indirect
	github.com/oapi-codegen/runtime v1.0.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.38.2 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
)

replace Piranid/pkg => ../../pkg
