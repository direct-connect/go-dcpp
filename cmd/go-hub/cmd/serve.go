package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/hub"
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

type Config struct {
	Name    string `yaml:"name"`
	Desc    string `yaml:"desc"`
	Website string `yaml:"website"`
	Email   string `yaml:"email"`
	MOTD    string `yaml:"motd"`
	Serve   struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"serve"`
}

const defaultConfig = "hub.yml"

func initConfig(path string) error {
	return viper.WriteConfigAs(path)
}

func readConfig() (*Config, error) {
	err := viper.ReadInConfig()
	if err == nil {
		fmt.Println("loaded config:", viper.ConfigFileUsed())
	}
	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		if err = initConfig(defaultConfig); err != nil {
			return nil, err
		}
		err = viper.ReadInConfig()
		if err == nil {
			fmt.Println("initialized config:", viper.ConfigFileUsed())
		}
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := viper.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func init() {
	viper.AddConfigPath(".")
	if runtime.GOOS != "windows" {
		viper.AddConfigPath("/etc/go-hub")
	}
	viper.SetConfigName("hub")
	viper.SetDefault("motd", "Welcome!")

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
	viper.BindPFlag("name", flags.Lookup("name"))
	flags.String("desc", "Hybrid hub", "description of the hub")
	viper.BindPFlag("desc", flags.Lookup("desc"))
	flags.String("host", "127.0.0.1", "host or IP to sign TLS certs for")
	viper.BindPFlag("serve.host", flags.Lookup("host"))
	flags.Int("port", 1411, "port to listen on")
	viper.BindPFlag("serve.port", flags.Lookup("port"))
	Root.AddCommand(serveCmd)

	serveCmd.RunE = func(cmd *cobra.Command, args []string) error {
		conf, err := readConfig()
		if err != nil {
			return err
		}
		cert, kp, err := loadCert(conf.Serve.Host)
		if err != nil {
			return err
		}

		tlsConf := &tls.Config{
			Certificates: []tls.Certificate{*cert},
		}
		host := ":" + strconv.Itoa(conf.Serve.Port)
		addr := conf.Serve.Host + host
		fmt.Println("listening on", host)

		if *fDebug {
			fmt.Println("WARNING: protocol debug enabled")
			nmdc.Debug = true
			adc.Debug = true
		}

		if *fPProf {
			const pprofPort = ":6060"
			fmt.Println("enabling profiler on", pprofPort)
			go func() {
				if err := http.ListenAndServe(pprofPort, nil); err != nil {
					log.Println("cannot enable profiler:", err)
				}
			}()
		}

		h := hub.NewHub(hub.Info{
			Name:    conf.Name,
			Desc:    conf.Desc,
			Website: conf.Website,
			Email:   conf.Email,
			MOTD:    conf.MOTD,
			Addr:    addr,
		}, tlsConf)

		fmt.Printf(`

[ Hub URIs ]
adcs://%s?kp=%s
adcs://%s
adc://%s
dchub://%s

[ IRC chat ]
ircs://%s/hub
irc://%s/hub

[ HTTPS stats ]
https://%s%s

`,
			addr, kp,
			addr,
			addr,
			addr,

			addr,
			addr,

			addr, hub.HTTPInfoPathV0,
		)
		return h.ListenAndServe(host)
	}
}
