package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
	"github.com/kelseyhightower/envconfig"
	"golang.org/x/net/context"
)

// config options set by env vars
type options struct {
	DataDir          string `envconfig:"data_dir"`
	LogLevel         string
	Path             string
	Discovery        string
	Peers            string
	Members          int
	Name             string
	ListenPeerURLs   string `envconfig:"listen_peer_urls"`
	ListenClientURLs string `envconfig:"listen_client_urls"`
}

//config we build from env.
type config struct {
	DataDir          string
	Path             string
	Discovery        string
	Peers            []string
	Members          int
	Name             string
	ListenPeerURLs   []string
	ListenClientURLs []string
	Addresses        []string
	env              map[string]string
}

func main() {
	o := options{
		DataDir:  "/var/lib/etcd",
		LogLevel: "info",
		Members:  5,
	}

	if err := envconfig.Process("etcd", &o); err != nil {
		log.Fatal(err)
	}

	c, err := o.config()
	if err != nil {
		log.Fatal(err)
	}

	if c.Discovery != "" {
		log.Infof("starting etcd as ETCD_DISCOVERY is set to %s", c.Discovery)
		c.env["ETCD_DISCOVERY"] = c.Discovery
		c.runEtcd()
	}

	for _, d := range []string{"member", "proxy"} {
		path := filepath.Join(c.DataDir, d)
		_, err := os.Stat(path)
		if err == nil {
			log.Infof("starting etcd as %s exists", path)
			c.runEtcd()
		}
		if !os.IsNotExist(err) {
			log.Fatal(err)
		}
	}

	if err := c.setPeers(o.Peers); err != nil {
		log.Fatal(err)
	}

	members, err := c.getMembers()
	if err != nil {
		log.Fatal(err)
	}

	//already a member?
	for _, member := range members {
		if member.Name == c.Name {
			log.Info("starting etcd as %s is already a member", c.Name)
			c.runEtcd()
		}
	}

	if err := c.setInitialCluster(members); err != nil {
		log.Fatal(err)
	}

	// have enough members?
	if len(members) >= c.Members {
		log.Info("starting etcd in proxy mode as there are %d members", len(members))
		c.env["ETCD_PROXY"] = "on"
		c.runEtcd()
	}

	if err := c.addMember(); err != nil {
		log.Fatalf("failed to add new member: %s", err)
	}

	c.env["ETCD_INITIAL_CLUSTER_STATE"] = "existing"

	c.runEtcd()
	log.Fatal("unreachable state")
}

func (c *config) setInitialCluster(members []client.Member) error {
	members = append(members, client.Member{
		Name:     c.Name,
		PeerURLs: c.ListenPeerURLs,
	})

	conf := []string{}
	for _, memb := range members {
		for _, u := range memb.PeerURLs {
			conf = append(conf, fmt.Sprintf("%s=%s", memb.Name, u))
		}
	}
	c.env["ETCD_INITIAL_CLUSTER"] = strings.Join(conf, ",")
	return nil
}

func (c *config) runEtcd() {
	c.env["ETCD_DATA_DIR"] = c.DataDir
	c.env["ETCD_NAME"] = c.Name
	c.env["ETCD_LISTEN_PEER_URLS"] = strings.Join(c.ListenPeerURLs, ",")
	c.env["ETCD_LISTEN_CLIENT_URLS"] = strings.Join(c.ListenClientURLs, ",")
	c.env["ETCD_ADVERTISE_CLIENT_URLS"] = strings.Join(c.ListenClientURLs, ",")

	var envv []string
	for k, v := range c.env {
		log.Infof("%s = %s", k, v)
		envv = append(envv, strings.Join([]string{k, v}, "="))
	}
	if err := syscall.Exec(c.Path, []string{"etcd"}, envv); err != nil {
		log.Fatal(err)
	}
	// never reached
	os.Exit(0)
}

func (c *config) addMember() error {
	cfg := client.Config{
		Endpoints: c.Peers,
	}
	cl, err := client.New(cfg)
	if err != nil {
		return err
	}

	m := client.NewMembersAPI(cl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = m.Add(ctx, strings.Join(c.ListenPeerURLs, ","))
	return err
}

func (c *config) getMembers() ([]client.Member, error) {
	cfg := client.Config{
		Endpoints: c.Peers,
	}
	cl, err := client.New(cfg)
	if err != nil {
		return nil, err
	}

	m := client.NewMembersAPI(cl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.List(ctx)
}

func (c *config) getAddresses() ([]string, error) {
	if c.Addresses == nil || len(c.Addresses) == 0 {
		var ips []string
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				// log error?
				continue
			}
			if ip.To4() == nil {
				continue
			}
			if ip.IsLoopback() || ip.IsGlobalUnicast() {
				ips = append(ips, ip.String())
			}
		}

		if ips == nil || len(ips) == 0 {
			return nil, errors.New("did not find any valid addresses")
		}

		c.Addresses = ips
	}
	return c.Addresses, nil

}

func (c *config) setClientURLS(u string) error {
	if u != "" {
		c.ListenClientURLs = strings.Split(u, ",")
		return nil
	}
	addrs, err := c.getAddresses()
	if err != nil {
		return err
	}
	var urls []string
	for _, ip := range addrs {
		urls = append(urls, fmt.Sprintf("http://%s", net.JoinHostPort(ip, "2380")))
	}
	c.ListenClientURLs = urls

	return nil
}

func (c *config) setPeerURLs(u string) error {
	if u != "" {
		c.ListenPeerURLs = strings.Split(u, ",")
		return nil
	}
	addrs, err := c.getAddresses()
	if err != nil {
		return err
	}
	var urls []string
	for _, ip := range addrs {
		urls = append(urls, fmt.Sprintf("http://%s", net.JoinHostPort(ip, "2379")))
	}
	c.ListenPeerURLs = urls

	return nil
}

func (c *config) setName() error {
	if c.Name == "" {
		hn, err := os.Hostname()
		if err != nil {
			return err
		}
		c.Name = hn
	}
	c.Name = strings.Split(c.Name, ".")[0]
	return nil
}

func setLogLevel(l string) error {
	level, err := log.ParseLevel(l)
	if err != nil {
		return err
	}
	log.SetLevel(level)
	return nil
}

func (c *config) setPeers(peers string) error {
	if peers == "" {
		return errors.New("peers cannot be blank")
	}
	c.Peers = strings.Split(peers, ",")
	return nil
}

func (c *config) setPath() error {
	if c.Path == "" {
		path, err := exec.LookPath("etcd")
		if err != nil {
			return err
		}
		c.Path = path
	}
	return nil

}

// generate a config from options
func (o *options) config() (*config, error) {
	if err := setLogLevel(o.LogLevel); err != nil {
		return nil, fmt.Errorf("failed to set log level: %s", err)
	}

	c := config{
		DataDir:   o.DataDir,
		Path:      o.Path,
		Discovery: o.Discovery,
		Members:   o.Members,
		Name:      o.Name,
		env:       make(map[string]string),
	}

	if err := c.setName(); err != nil {
		return nil, fmt.Errorf("failed to set name: %s", err)
	}

	if err := c.setPath(); err != nil {
		return nil, fmt.Errorf("failed to set etcd path: %s", err)
	}

	if err := c.setClientURLS(o.ListenClientURLs); err != nil {
		return nil, fmt.Errorf("failed to set client URLs: %s", err)
	}

	if err := c.setPeerURLs(o.ListenPeerURLs); err != nil {
		return nil, fmt.Errorf("failed to set peer URLs: %s", err)
	}

	return &c, nil
}
