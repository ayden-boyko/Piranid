module github.com/ayden-boyko/Piranid/nodes/Logging

replace github.com/ayden-boyko/Piranid/internal/node => ../../internal/node

go 1.23

require (
	github.com/ayden-boyko/Piranid/internal/node v0.0.0-00010101000000-000000000000
	github.com/influxdata/influxdb-client-go/v2 v2.14.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/google/uuid v1.3.1 // indirect
	github.com/influxdata/line-protocol v0.0.0-20200327222509-2487e7298839 // indirect
	github.com/oapi-codegen/runtime v1.0.0 // indirect
	github.com/redis/go-redis/v9 v9.8.0 // indirect
	golang.org/x/net v0.23.0 // indirect
)
