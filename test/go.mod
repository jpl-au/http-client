module http-client/test

go 1.25

require (
	github.com/andybalholm/brotli v1.2.0
	github.com/golang/snappy v0.0.4
	github.com/jpl-au/http-client v1.0.0
	github.com/pierrec/lz4/v4 v4.1.22
	github.com/stretchr/testify v1.10.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
)

replace github.com/jpl-au/http-client => ../
