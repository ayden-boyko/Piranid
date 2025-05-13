module github.com/ayden-boyko/Piranid/nodes/Notifications

replace github.com/ayden-boyko/Piranid/internal/node => ../../internal/node

go 1.23

require github.com/ayden-boyko/Piranid/internal/node v0.0.0-00010101000000-000000000000

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/redis/go-redis/v9 v9.8.0 // indirect
)
