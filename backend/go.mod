module github.com/fprambs/transferly

go 1.27.4

require (
	github.com/bodgit/sevenzip v1.6.5
	github.com/coder/websocket v1.8.15
	github.com/fclairamb/ftpserverlib v0.32.7
	github.com/fxamacker/cbor/v2 v2.10.0
	github.com/getkin/kin-openapi v0.152.0
	github.com/go-chi/chi/v5 v5.5.0
	github.com/go-webauthn/webauthn v0.18.2
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.12.0
	github.com/jlaffaye/ftp v0.2.4
	github.com/johannesboyne/gofakes3 v1.2.0
	github.com/klauspost/compress v1.20.3
	github.com/minio/minio-go/v7 v7.4.1
	github.com/nwaples/rardecode/v2 v2.4.6
	github.com/oapi-codegen/runtime v1.8.0
	github.com/pkg/sftp v1.13.12
	github.com/pressly/goose/v3 v3.29.0
	github.com/spf13/afero v1.15.0
	github.com/ulikunitz/xz v0.5.17
	golang.org/x/crypto v0.59.0
	golang.org/x/net v0.62.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/text v0.44.0
)

require (
	github.com/andybalholm/brotli v1.2.8 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/go-openapi/jsonpointer v1.0.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/klauspost/crc32 v1.3.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/minio/crc64nvme v1.1.1 // indirect
	github.com/minio/md5-simd v1.1.2 // indirect
	github.com/oasdiff/yaml v0.1.3 // indirect
	github.com/oasdiff/yaml3 v0.0.16 // indirect
	github.com/philhofer/fwd v1.2.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/ryszard/goskiplist v0.0.0-20150312221310-2dfbae5fcf46 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/stangelandcl/ppmd v0.1.1 // indirect
	github.com/tinylib/msgp v1.6.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.shabbyrobe.org/gocovmerge v0.0.0-20230507111327-fa4f82cfbf4d // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	go4.org v0.0.0-20261028031214-ed6929a230fe // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	gopkg.in/ini.v1 v1.67.3 // indirect
)

// The FTP client carries a patch that answers servers asking for an account (ACCT); see third_party/ftp/PATCHES.md.
replace github.com/jlaffaye/ftp => ./third_party/ftp

// The 7z parser bounds hostile header allocations and decoder dictionaries; see third_party/sevenzip/PATCHES.md.
replace github.com/bodgit/sevenzip => ./third_party/sevenzip
