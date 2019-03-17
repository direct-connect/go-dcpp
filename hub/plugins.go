package hub

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"plugin"
	"runtime"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
	Rev   string
}

func (v Version) String() string {
	s := "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Rev != "" {
		s += "-" + v.Rev
	}
	return s
}

var (
	pluginsByName = make(map[string]Plugin)
)

type Plugin interface {
	// Name is a unique name of a plugin.
	Name() string
	// Version returns a plugin version.
	Version() Version
	// Init the plugin for a given hub. The plugin should save the reference to
	// the hub to be able to call methods on it.
	Init(h *Hub) error
	// Close shuts down a plugin.
	Close() error
}

// RegisterPlugin should be called to register a new hub plugin. When the hub is started,
// p.Init will be called to associate the plugin with a hub.
//
// This function should be called in the plugin's init() function.
func RegisterPlugin(p Plugin) {
	if p == nil {
		panic("plugin should not be nil")
	}
	name := p.Name()
	if name != strings.TrimSpace(name) {
		panic("plugin name should not start or end with space")
	}
	m2, ok := pluginsByName[name]
	if ok {
		panic(fmt.Errorf("plugin %q is already registered: %v vs %v", name, p.Version(), m2.Version()))
	}
	pluginsByName[name] = p
}

func (h *Hub) initPlugins() error {
	for _, p := range pluginsByName {
		err := p.Init(h)
		if err != nil {
			h.stopPlugins()
			return err
		}
		fmt.Printf("plugin loaded: %s (%v)\n", p.Name(), p.Version())
		h.plugins.loaded = append(h.plugins.loaded, p)
	}
	return nil
}

func (h *Hub) stopPlugins() {
	for _, p := range h.plugins.loaded {
		err := p.Close()
		if err != nil {
			fmt.Printf("error stopping the plugin %s: %v\n", p.Name(), err)
		}
	}
}

// LoadPluginsInDir loads all plugins in a specified directory. Should be called before Start.
//
// Details about building Go plugins can be found here: https://golang.org/pkg/plugin/
func (h *Hub) LoadPluginsInDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()

	var ext string
	if runtime.GOOS == "windows" {
		ext = ".dll"
	} else {
		ext = ".so"
	}
	for {
		names, err := d.Readdirnames(100)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ext) {
				continue
			}
			// only init() matters
			_, err := plugin.Open(filepath.Join(dir, name))
			if err != nil {
				return err
			}
		}
	}
}
