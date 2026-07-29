module github.com/direct-connect/go-dcpp

go 1.25.0

require (
	github.com/Shopify/go-lua v0.0.0-20181106184032-48449c60c0a9
	github.com/blang/semver v3.5.1+incompatible
	github.com/direct-connect/go-dc v0.10.3
	github.com/go-irc/irc v2.1.0+incompatible
	github.com/hidal-go/hidalgo v0.0.0-20190420191634-c112d74960ad
	github.com/prometheus/client_golang v1.23.2
	github.com/rakyll/statik v0.1.6
	github.com/spf13/cobra v0.0.3
	github.com/spf13/viper v1.3.2
	github.com/stretchr/testify v1.11.1
	golang.org/x/net v0.57.0
	golang.org/x/text v0.40.0
)

require (
	github.com/AndreasBriese/bbloom v0.0.0-20190306092124-e2d15f34fcf9 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/bbolt v1.3.2 // indirect
	github.com/cxmcc/tiger v0.0.0-20170524142333-bde35e2713d7 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dennwc/base v1.0.0 // indirect
	github.com/dgraph-io/badger v1.5.4 // indirect
	github.com/dgryski/go-farm v0.0.0-20190416075124-e1214b5e05dc // indirect
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/gogo/protobuf v1.2.1 // indirect
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/magiconair/properties v1.8.0 // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml v1.3.0 // indirect
	github.com/pkg/errors v0.8.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/spf13/afero v1.2.2 // indirect
	github.com/spf13/cast v1.3.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.3 // indirect
	github.com/syndtr/goleveldb v1.0.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/yaml.v2 v2.2.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/Shopify/go-lua => github.com/direct-connect/go-lua v0.0.0-20190505214648-cdda08bfc989

replace github.com/direct-connect/go-dc => ../go-dc
