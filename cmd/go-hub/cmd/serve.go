package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"io/ioutil"
	"runtime"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	_ "github.com/direct-connect/go-dcpp/hub/plugins/all"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/hub"
	"github.com/direct-connect/go-dcpp/hub/hubdb"
	"github.com/direct-connect/go-dcpp/nmdc"
	"github.com/direct-connect/go-dcpp/version"
)

const Version = version.Vers

var Root = &cobra.Command{
	Use: "go-hub <command>",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Version:\t%s\nGo runtime:\t%s\n\n",
			Version, runtime.Version(),
		)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "run the hub",
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "configure the hub",
}

var confManager *viper.Viper // pointer to config manager

type Config struct {
	Hub     struct {
		Name    string `yaml:"name"`
		Desc    string `yaml:"desc"`
		Owner   string `yaml:"owner"`
		Website string `yaml:"website"`
		Email   string `yaml:"email"`
		Icon    string `yaml:"icon"`
		Logo    string `yaml:"logo"`
		MOTD    string `yaml:"motd"`
		Private bool   `yaml:"private"`
	} `yaml:"hub"`

	Bot     struct {
		Name string     `yaml:"name"`
		Desc string     `yaml:"desc"`
	} `yaml:"bot"`

	OpChat     struct {
		Name string     `yaml:"name"`
		Desc string     `yaml:"desc"`
	} `yaml:"opchat"`

	Serve   struct {
		Host string     `yaml:"host"`
		Port int        `yaml:"port"`
		TLS  *TLSConfig `yaml:"tls"`
	} `yaml:"serve"`

	Chat struct {
		Encoding string `yaml:"encoding"`
		Log      struct {
			Max  int `yaml:"max"`
			Join int `yaml:"join"`
		}
	} `yaml:"chat"`

	Database struct {
		Type string `yaml:"type"`
		Path string `yaml:"path"`
	} `yaml:"database"`

	Plugins struct {
		Path string `yaml:"path"`
	} `yaml:"plugins"`
}

const defaultConfig = "hub.yml"

func initConfig(path string) error {
	return confManager.WriteConfigAs(path)
}

func readConfig(create bool) (*Config, hub.Map, error) {
	err := confManager.ReadInConfig()
	if err == nil {
		log.Printf("loaded config: %s\n", confManager.ConfigFileUsed())
	}
	if _, ok := err.(viper.ConfigFileNotFoundError); ok && create {
		if err = initConfig(defaultConfig); err != nil {
			return nil, nil, err
		}
		err = confManager.ReadInConfig()
		if err == nil {
			log.Println("initialized config:", confManager.ConfigFileUsed())
		}
	}
	if err != nil {
		return nil, nil, err
	}
	var c Config
	if err := confManager.Unmarshal(&c); err != nil {
		return nil, nil, err
	}
	var m map[string]interface{}
	if err := confManager.Unmarshal(&m); err != nil {
		return nil, nil, err
	}
	return &c, hub.Map(m), nil
}

func init() {
	confManager = viper.New()
	confManager.AddConfigPath(".")

	if runtime.GOOS != "windows" {
		confManager.AddConfigPath("/etc/go-hub")
	}

	motd := "motd.txt" // motd file name
	confManager.SetConfigName("hub")
	confManager.SetDefault("hub.motd", motd)
	confManager.SetDefault("hub.icon", "icon.png")
	confManager.SetDefault("hub.private", false)
	confManager.SetDefault("chat.encoding", "cp1251")
	confManager.SetDefault("chat.log.max", 50)
	confManager.SetDefault("chat.log.join", 10)
	confManager.SetDefault("database.type", "bolt")
	confManager.SetDefault("database.path", "hub.db")
	confManager.SetDefault("plugins.path", "plugins")

	if _, err := os.Stat(motd); os.IsNotExist(err) { // create motd
		err = ioutil.WriteFile(motd, []byte(`

 .:: Welcome %[USER_NAME] to %[HUB_NAME]

 .:: Hub information ::.

 .:: Online users: %[HUB_USERS]
 .:: Total share: %[HUB_SHARE]
 .:: Uptime: %[HUB_UPTIME]

 .:: Your information ::.

 .:: IP address: %[USER_ADDR]
 .:: Share size: %[USER_SHARE]

 .:: Don't forget to add this hub to favorites: /fav
`), 0600)

		if err != nil {
			log.Printf("Failed to create %s: %v\n", motd, err)
		}
	}

	initCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := initConfig(defaultConfig); err != nil {
			return err
		}
		fmt.Println("initialized config:", defaultConfig)
		return nil
	}
	Root.AddCommand(initCmd)

	flags := serveCmd.Flags()

	fDebug := flags.Bool("debug", false, "print protocol logs to stderr")
	fPProf := flags.Bool("pprof", false, "enable profiler endpoint")

	flags.String("name", "GoHub", "name of the hub")
	confManager.BindPFlag("hub.name", flags.Lookup("name"))
	flags.String("desc", "Hybrid hub", "description of the hub")
	confManager.BindPFlag("hub.desc", flags.Lookup("desc"))
	flags.String("host", "127.0.0.1", "host or IP to sign TLS certs for")
	confManager.BindPFlag("serve.host", flags.Lookup("host"))
	flags.Int("port", 1411, "port to listen on")
	confManager.BindPFlag("serve.port", flags.Lookup("port"))
	flags.String("plugins", "plugins", "directory for hub plugins")
	confManager.BindPFlag("plugins.path", flags.Lookup("plugins"))
	Root.AddCommand(serveCmd)

	serveCmd.RunE = func(cmd *cobra.Command, args []string) error {
		conf, cmap, err := readConfig(true)
		if err != nil {
			return err
		}

		noTLS := conf.Serve.TLS == nil
		cert, kp, err := loadCert(conf)
		if err != nil {
			return err
		}
		if noTLS {
			confManager.Set("serve.tls", conf.Serve.TLS)
			if err = confManager.WriteConfig(); err != nil {
				return err
			}
		}

		tlsConf := &tls.Config{
			Certificates: []tls.Certificate{*cert},
		}
		host := ":" + strconv.Itoa(conf.Serve.Port)
		addr := conf.Serve.Host + host

		if conf.Chat.Encoding != "" {
			fmt.Println("fallback encoding:", conf.Chat.Encoding)
		}
		h, err := hub.NewHub(hub.Config{
			Name:             conf.Hub.Name,
			Desc:             conf.Hub.Desc,
			Owner:            conf.Hub.Owner,
			Website:          conf.Hub.Website,
			Email:            conf.Hub.Email,
			Icon:             conf.Hub.Icon,
			Logo:             conf.Hub.Logo,
			MOTD:             conf.Hub.MOTD,
			Private:          conf.Hub.Private,
			BotName:          conf.Bot.Name,
			BotDesc:          conf.Bot.Desc,
			OpChatName:       conf.OpChat.Name,
			OpChatDesc:       conf.OpChat.Desc,
			FallbackEncoding: conf.Chat.Encoding,
			ChatLog:          conf.Chat.Log.Max,
			ChatLogJoin:      conf.Chat.Log.Join,
			Addr:             addr,
			TLS:              tlsConf,
			Keyprint:         kp,
		}, confManager) // TODO: this shouldnt have a dependency on viper, it might be better to pass required fields as a part of the config
		if err != nil {
			return err
		}
		h.MergeConfig(cmap)

		if *fDebug {
			log.Println("WARNING: protocol debug enabled")
			nmdc.Debug = true
			adc.Debug = true
		}

		if *fPProf {
			const pprofPort = ":6060"
			log.Println("enabling profiler on", pprofPort)
			go func() {
				if err := http.ListenAndServe(pprofPort, nil); err != nil {
					log.Println("cannot enable profiler:", err)
				}
			}()
		}
		if true {
			const promAddr = ":2112"
			log.Println("serving metrics on", promAddr)
			go func() {
				if err := http.ListenAndServe(promAddr, promhttp.Handler()); err != nil {
					log.Println("cannot serve metrics:", err)
				}
			}()
		}
		if conf.Database.Type != "" && conf.Database.Type != "mem" {
			log.Printf("using database: %s (%s)\n", conf.Database.Path, conf.Database.Type)
			db, err := hubdb.Open(conf.Database.Type, conf.Database.Path)
			if err != nil {
				return err
			}
			defer db.Close()
			h.SetDatabase(db)
		} else {
			log.Println("WARNING: using in-memory database")
		}

		if _, err := os.Stat(conf.Plugins.Path); err == nil {
			log.Println("loading plugins in:", conf.Plugins.Path)
			if err := h.LoadPluginsInDir(conf.Plugins.Path); err != nil {
				return err
			}
		}

		if err := setLimits(); err != nil {
			return err
		}

		if err := h.Start(); err != nil {
			return err
		}
		defer h.Close()

		log.Println("listening on", host)

		fmt.Printf(`
[ Hub URIs ]
adcs://%s?kp=%s
adcs://%s
adc://%s
nmdcs://%s
dchub://%s

[ IRC chat ]
ircs://%s/hub
irc://%s/hub

[ HTTP stats ]
https://%s%s
http://%s%s

`,
			addr, kp,
			addr,
			addr,
			addr,
			addr,

			addr,
			addr,

			addr, hub.HTTPInfoPathV0,
			addr, hub.HTTPInfoPathV0,
		)

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		go func() {
			<-ch
			log.Println("stopping server")
			h.SendGlobalChat("Stopping hub")
			_ = h.Close()
		}()

		Root.SilenceUsage = true
		return h.ListenAndServe(host)
	}
}
