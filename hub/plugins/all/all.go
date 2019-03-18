package all

import (
	_ "github.com/direct-connect/go-dcpp/hub/plugins/hubstats"
	_ "github.com/direct-connect/go-dcpp/hub/plugins/myip"

	// LUA is loaded the last
	_ "github.com/direct-connect/go-dcpp/hub/plugins/lua"
)
