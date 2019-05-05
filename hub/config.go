package hub

import (
	"fmt"
	"sort"
	"strconv"
)

const (
	ConfigHubName    = "hub.name"
	ConfigHubDesc    = "hub.desc"
	ConfigHubTopic   = "hub.topic"
	ConfigHubOwner   = "hub.owner"
	ConfigHubWebsite = "hub.website"
	ConfigHubEmail   = "hub.email"
	ConfigHubMOTD    = "hub.motd"
)

var configAliases = map[string]string{
	"name":    ConfigHubName,
	"desc":    ConfigHubDesc,
	"topic":   ConfigHubTopic,
	"owner":   ConfigHubOwner,
	"website": ConfigHubWebsite,
	"email":   ConfigHubEmail,
	"motd":    ConfigHubMOTD,
}

// configIgnored is a list of ignored config keys that can only be set in the config file.
var configIgnored = map[string]struct{}{
	"chat.encoding":  {},
	"chat.log.join":  {},
	"chat.log.max":   {},
	"database.path":  {},
	"database.type":  {},
	"plugins.path":   {},
	"serve.host":     {},
	"serve.port":     {},
	"serve.tls.cert": {},
	"serve.tls.key":  {},
}

func (h *Hub) MergeConfig(m Map) {
	h.MergeConfigPath("", m)
}

func (h *Hub) MergeConfigPath(path string, m Map) {
	for k, v := range m {
		if path != "" {
			k = path + "." + k
		}
		switch v := v.(type) {
		case Map:
			h.MergeConfigPath(k, v)
		case map[string]interface{}:
			h.MergeConfigPath(k, Map(v))
		default:
			h.setConfig(k, v, false)
		}
	}
}

func (h *Hub) saveConfig(key string, val interface{}) {
	if _, ok := configIgnored[key]; ok {
		return
	}
	// TODO: persist config
}

func (h *Hub) setConfigMap(key string, val interface{}) {
	if _, ok := configIgnored[key]; ok {
		return
	}
	h.conf.Lock()
	if h.conf.m == nil {
		h.conf.m = make(Map)
	}
	h.conf.m[key] = val
	h.conf.Unlock()
}

func (h *Hub) getConfigMap(key string) (interface{}, bool) {
	h.conf.RLock()
	val, ok := h.conf.m[key]
	h.conf.RUnlock()
	return val, ok
}

func (h *Hub) setConfig(key string, val interface{}, save bool) {
	if _, ok := configIgnored[key]; ok {
		return
	}
	switch val := val.(type) {
	case bool:
		h.setConfigBool(key, val)
	case string:
		h.setConfigString(key, val)
	case int:
		h.setConfigInt(key, int64(val))
	case int64:
		h.setConfigInt(key, val)
	case int32:
		h.setConfigInt(key, int64(val))
	case uint:
		h.setConfigUint(key, uint64(val))
	case uint64:
		h.setConfigUint(key, val)
	case uint32:
		h.setConfigUint(key, uint64(val))
	case float64:
		h.setConfigFloat(key, val)
	case float32:
		h.setConfigFloat(key, float64(val))
	default:
		panic(fmt.Errorf("unsupported config type: %T", val))
	}
	if save {
		h.saveConfig(key, val)
	}
}

func (h *Hub) SetConfig(key string, val interface{}) {
	h.setConfig(key, val, true)
}

func (h *Hub) ConfigKeys() []string {
	keys := []string{
		ConfigHubName,
		ConfigHubDesc,
		ConfigHubTopic,
		ConfigHubMOTD,
		ConfigHubOwner,
		ConfigHubWebsite,
		ConfigHubEmail,
	}
	h.conf.RLock()
	for k := range h.conf.m {
		if _, ok := configIgnored[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	h.conf.RUnlock()
	sort.Strings(keys)
	return keys
}

func (h *Hub) GetConfig(key string) (interface{}, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	case ConfigHubName,
		ConfigHubDesc,
		ConfigHubTopic,
		ConfigHubMOTD,
		ConfigHubOwner,
		ConfigHubWebsite,
		ConfigHubEmail:
		v, ok := h.GetConfigString(key)
		if !ok {
			return nil, false
		}
		return v, true
	}
	h.conf.RLock()
	v, ok := h.conf.m[key]
	h.conf.RUnlock()
	return v, ok && v != nil
}

func (h *Hub) setConfigString(key string, val string) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	case ConfigHubName:
		h.setName(val)
	case ConfigHubDesc:
		h.setDesc(val)
	case ConfigHubTopic:
		h.setTopic(val)
	case ConfigHubMOTD:
		h.setMOTD(val)
	case ConfigHubOwner:
		h.conf.Lock()
		h.conf.Owner = val
		h.conf.Unlock()
	case ConfigHubWebsite:
		h.conf.Lock()
		h.conf.Website = val
		h.conf.Unlock()
	case ConfigHubEmail:
		h.conf.Lock()
		h.conf.Email = val
		h.conf.Unlock()
	default:
		h.setConfigMap(key, val)
	}
}

func (h *Hub) SetConfigString(key string, val string) {
	h.setConfigString(key, val)
	h.saveConfig(key, val)
}

func (h *Hub) GetConfigString(key string) (string, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	case ConfigHubName:
		return h.getName(), true
	case ConfigHubDesc:
		h.conf.RLock()
		v := h.conf.Owner
		h.conf.RUnlock()
		return v, true
	case ConfigHubTopic:
		h.conf.RLock()
		v := h.conf.Topic
		h.conf.RUnlock()
		return v, true
	case ConfigHubMOTD:
		h.conf.RLock()
		v := h.conf.MOTD
		h.conf.RUnlock()
		return v, true
	case ConfigHubOwner:
		h.conf.RLock()
		v := h.conf.Owner
		h.conf.RUnlock()
		return v, true
	case ConfigHubWebsite:
		h.conf.RLock()
		v := h.conf.Website
		h.conf.RUnlock()
		return v, true
	case ConfigHubEmail:
		h.conf.RLock()
		v := h.conf.Email
		h.conf.RUnlock()
		return v, true
	default:
		v, ok := h.getConfigMap(key)
		if !ok || v == nil {
			return "", false
		}
		switch v := v.(type) {
		case string:
			return v, true
		default:
			return fmt.Sprint(v), true
		}
	}
}

func (h *Hub) setConfigBool(key string, val bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	if _, ok := configIgnored[key]; ok {
		return
	}
	switch key {
	default:
		h.setConfigMap(key, val)
	}
}

func (h *Hub) SetConfigBool(key string, val bool) {
	h.setConfigBool(key, val)
	h.saveConfig(key, val)
}

func (h *Hub) GetConfigBool(key string) (bool, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	default:
		v, ok := h.getConfigMap(key)
		if !ok || v == nil {
			return false, false
		}
		switch v := v.(type) {
		case bool:
			return v, true
		case int64:
			return v != 0, true
		case uint64:
			return v != 0, true
		case float64:
			return v != 0, true
		case string:
			b, _ := strconv.ParseBool(v)
			return b, true
		default:
			return false, true
		}
	}
}

func (h *Hub) setConfigInt(key string, val int64) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	if _, ok := configIgnored[key]; ok {
		return
	}
	switch key {
	default:
		h.setConfigMap(key, val)
	}
}

func (h *Hub) SetConfigInt(key string, val int64) {
	h.setConfigInt(key, val)
	h.saveConfig(key, val)
}

func (h *Hub) GetConfigInt(key string) (int64, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	default:
		v, ok := h.getConfigMap(key)
		if !ok || v == nil {
			return 0, false
		}
		switch v := v.(type) {
		case int64:
			return v, true
		case uint64:
			return int64(v), true
		case float64:
			return int64(v), true
		case bool:
			if v {
				return 1, true
			}
			return 0, true
		case string:
			i, _ := strconv.ParseInt(v, 10, 64)
			return i, true
		default:
			return 0, true
		}
	}
}

func (h *Hub) setConfigUint(key string, val uint64) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	if _, ok := configIgnored[key]; ok {
		return
	}
	switch key {
	default:
		h.setConfigMap(key, val)
	}
}

func (h *Hub) SetConfigUint(key string, val uint64) {
	h.setConfigUint(key, val)
	h.saveConfig(key, val)
}

func (h *Hub) GetConfigUint(key string) (uint64, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	default:
		v, ok := h.getConfigMap(key)
		if !ok || v == nil {
			return 0, false
		}
		switch v := v.(type) {
		case uint64:
			return v, true
		case int64:
			return uint64(v), true
		case float64:
			return uint64(v), true
		case bool:
			if v {
				return 1, true
			}
			return 0, true
		case string:
			i, _ := strconv.ParseUint(v, 10, 64)
			return i, true
		default:
			return 0, true
		}
	}
}

func (h *Hub) setConfigFloat(key string, val float64) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	if _, ok := configIgnored[key]; ok {
		return
	}
	switch key {
	default:
		h.setConfigMap(key, val)
	}
}

func (h *Hub) SetConfigFloat(key string, val float64) {
	h.setConfigFloat(key, val)
	h.saveConfig(key, val)
}

func (h *Hub) GetConfigFloat(key string) (float64, bool) {
	if alias, ok := configAliases[key]; ok {
		key = alias
	}
	switch key {
	default:
		v, ok := h.getConfigMap(key)
		if !ok || v == nil {
			return 0, false
		}
		switch v := v.(type) {
		case uint64:
			return float64(v), true
		case int64:
			return float64(v), true
		case float64:
			return v, true
		case bool:
			if v {
				return 1, true
			}
			return 0, true
		case string:
			f, _ := strconv.ParseFloat(v, 64)
			return f, true
		default:
			return 0, true
		}
	}
}
