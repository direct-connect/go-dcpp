-- identify script

script = {
	name = "Test",
	version = "0.1.0.0"
}

--[[

versions = {
	lua = _VERSION,
	hub,
	hub_api,
	lua_plugin
}

user = {
	name,
	email,
	share = {
		bytes
	},
	soft = {
		name,
		vers
	},
	hubs = {
		normal,
		registered,
		operator
	},
	slots = {
		total
	},
	flags = {
		secure
	},
	address,
	sendGlobal (text)
}

hub = {
	info (),
	userByName (name),
	users (),
	sendGlobal (text)
}

]]--

-- define own

function _tostring (val)
	if type (val) == "number" then
		return string.format ("%d", val)
	end

	return tostring (val)
end

-- hub calls

hub.onTimer = function ()
	hub.sendGlobal ("onTimer")
end

hub.onConnected = function (addr)
	hub.sendGlobal ("onConnected," .. _tostring (addr))
	return 1
end

hub.onDisconnected = function (addr)
	hub.sendGlobal ("onDisconnected," .. _tostring (addr))
end

hub.onJoined = function (user)
	hub.sendGlobal ("onJoined")
	return 1
end

hub.onChat = function (stam, name, text, user)
	hub.sendGlobal ("onChat," .. _tostring (stam) .. "," .. _tostring (name) .. "," .. _tostring (text) .. "," .. _tostring (user))
	return 1
end

-- end of file