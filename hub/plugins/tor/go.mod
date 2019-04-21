module github.com/direct-connect/go-dcpp/hub/plugins/tor

go 1.12

require (
	github.com/cretz/bine v0.1.0
	github.com/direct-connect/go-dcpp v0.0.0-20190302204921-9f7b11a6b513
	github.com/ipsn/go-libtor v0.0.0-20190301223739-2ce4c3b6ee7b
	github.com/spf13/cobra v0.0.3
)

replace github.com/direct-connect/go-dcpp => ../../../
