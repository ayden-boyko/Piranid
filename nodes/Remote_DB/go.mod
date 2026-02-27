module github.com/ayden-boyko/Piranid/nodes/Remote_DB

go 1.24.9

require (
	Piranid/node v0.0.0-00010101000000-000000000000
	Piranid/pkg v0.0.0-00010101000000-000000000000
	github.com/go-redis/redis v6.15.9+incompatible
	modernc.org/sqlite v1.46.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-redis/redis/v8 v8.11.5
	github.com/google/pprof v0.0.0-20260115054156-294ebfa9ad83 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.39.1 // indirect
	github.com/redis/go-redis/v9 v9.8.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20251023183803-a4bb9ffd2546 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/tools v0.41.0 // indirect
	modernc.org/libc v1.67.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace Piranid/pkg => ../../pkg

replace Piranid/node => ../../pkg/node
