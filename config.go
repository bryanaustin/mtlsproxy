package main

import (
	"flag"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/bryanaustin/yaarp"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Profile struct {
	Name                string
	Listen              string
	Proxy               string //TODO: Rename to send
	Protocol            string
	ListenCertPath      string
	ListenCertRaw       string
	ListenPrivatePath   string
	ListenPrivateRaw    string
	ListenAuthorityPath string
	ListenAuthorityRaw  string
	SendCertPath        string
	SendCertRaw         string
	SendPrivatePath     string
	SendPrivateRaw      string
	SendAuthorityPath   string
	SendAuthorityRaw    string
	Source              string
}

type Configurations struct {
	ConfigDir string
	Profiles  []*Profile
}

const (
	EnvProfilePrefix         = "MTLSPROXY_PROFILE_"
	EnvProtocolSuffix        = "_PROTOCOL"
	EnvListenSuffix          = "_LISTEN"
	EnvProxySuffix           = "_PROXY"
	EnvListenCertSuffix      = "_CERT_LISTEN"
	EnvSendCertSuffix        = "_CERT_SEND"
	EnvListenPrivateSuffix   = "_PRIVATE_LISTEN"
	EnvSendPrivateSuffix     = "_PRIVATE_SEND"
	EnvAuthorityListenSuffix = "_AUTHORITY_LISTEN" //TODO: Rename _LISTEN_AUTHORITY
	EnvAuthoritySendSuffix   = "_AUTHORITY_SEND"
)

var (
	Debug bool
)

func (c Configurations) getProfiles() (nups []*Profile, err error) {
	nups = make([]*Profile, len(c.Profiles))
	for i := range nups {
		nups[i] = c.Profiles[i].Copy()
	}

	if len(c.ConfigDir) < 1 {
		return
	}

	var cfd *os.File
	cfd, err = os.Open(c.ConfigDir)
	if err != nil {
		err = fmt.Errorf("opening config directory: %w", err)
		return
	}
	var diritems []os.DirEntry
	for {
		diritems, err = cfd.ReadDir(16)
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			err = fmt.Errorf("reading contents of config directory: %w", err)
			return
		}
		for _, item := range diritems {
			if item.IsDir() {
				continue
			}

			var ps map[string]*Profile
			path := filepath.Join(c.ConfigDir, item.Name())
			_, err = toml.DecodeFile(path, &ps)
			if err != nil {
				err = fmt.Errorf("reading configuration %q: %w", path, err)
				return
			}

			pl := make([]*Profile, 0, len(ps))
			for k := range ps {
				ps[k].Name = k
				ps[k].Source = path
				pl = append(pl, ps[k])
			}

			nups = mergeProfiles(nups, pl...)
		}
	}

	return
}

func getImmutableConfigs() (c *Configurations, err error) {
	c = new(Configurations)
	flag.BoolVar(&Debug, "debug", false, "enable debug logging")
	flag.StringVar(&c.ConfigDir, "configdir", "", "directory for config files")
	yaarp.Parse()

	if env := os.Getenv("MTLSPROXY_DEBUG"); !Debug && len(env) > 0 {
		Debug, err = strconv.ParseBool(env)
		if err != nil {
			return
		}
	}

	if env := os.Getenv("MTLSPROXY_CONFIG_DIR"); len(c.ConfigDir) < 1 && len(env) > 0 {
		c.ConfigDir = env
	}

	c.Profiles = profilesFromEnv()
	return
}

func profilesFromEnv() (ps []*Profile) {
	allenvs := os.Environ()
	matchedPrefix := make([]string, 0, len(allenvs))

	findoradd := func(name string) *Profile {
		for i := range ps {
			if ps[i].Name == name {
				return ps[i]
			}
		}

		nu := &Profile{Name: name}
		ps = append(ps, nu)
		return nu
	}

	for _, x := range allenvs {
		if strings.HasPrefix(x, EnvProfilePrefix) {
			matchedPrefix = append(matchedPrefix, x[len(EnvProfilePrefix):])
		}
	}

	for _, x := range matchedPrefix {
		if r := profileSuffix(x, EnvListenSuffix); len(r) > 0 {
			p := findoradd(r)
			p.Listen = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvProxySuffix); len(r) > 0 {
			p := findoradd(r)
			p.Proxy = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvProtocolSuffix); len(r) > 0 {
			p := findoradd(r)
			p.Protocol = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvListenCertSuffix); len(r) > 0 {
			p := findoradd(r)
			p.ListenCertRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvSendCertSuffix); len(r) > 0 {
			p := findoradd(r)
			p.SendCertRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvListenPrivateSuffix); len(r) > 0 {
			p := findoradd(r)
			p.ListenPrivateRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvSendPrivateSuffix); len(r) > 0 {
			p := findoradd(r)
			p.SendPrivateRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvAuthorityListenSuffix); len(r) > 0 {
			p := findoradd(r)
			p.ListenAuthorityRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
		if r := profileSuffix(x, EnvAuthoritySendSuffix); len(r) > 0 {
			p := findoradd(r)
			p.SendAuthorityRaw = os.Getenv(EnvProfilePrefix + x)
			continue
		}
	}
	return
}

func mergeProfiles(b []*Profile, n ...*Profile) []*Profile {
	result := make([]*Profile, 0, len(b)+len(n))
	result = append(result, b...)
	for _, p := range n {
		found := -1
		for i := range result {
			if result[i].Name == p.Name {
				found = i
				break
			}
		}

		if found > -1 {
			result[found] = mergeProfile(result[found], p)
		} else {
			result = append(result, p)
		}
	}
	return result
}

func mergeProfile(a, b *Profile) *Profile {
	if a == nil {
		if b != nil {
			return b
		}
		a = new(Profile)
	}
	if b == nil {
		return a
	}

	if len(a.Listen) < 1 {
		a.Listen = b.Listen
	}
	if len(a.Proxy) < 1 {
		a.Proxy = b.Proxy
	}
	if len(a.Protocol) < 1 {
		a.Protocol = b.Protocol
	}
	if len(a.SendCertPath) < 1 {
		a.SendCertPath = b.SendCertPath
	}
	if len(a.SendCertRaw) < 1 {
		a.SendCertRaw = b.SendCertRaw
	}
	if len(a.ListenCertPath) < 1 {
		a.ListenCertPath = b.ListenCertPath
	}
	if len(a.ListenCertRaw) < 1 {
		a.ListenCertRaw = b.ListenCertRaw
	}
	if len(a.ListenPrivatePath) < 1 {
		a.ListenPrivatePath = b.ListenPrivatePath
	}
	if len(a.ListenPrivateRaw) < 1 {
		a.ListenPrivateRaw = b.ListenPrivateRaw
	}
	if len(a.SendPrivatePath) < 1 {
		a.SendPrivatePath = b.SendPrivatePath
	}
	if len(a.SendPrivateRaw) < 1 {
		a.SendPrivateRaw = b.SendPrivateRaw
	}
	if len(a.ListenAuthorityPath) < 1 {
		a.ListenAuthorityPath = b.ListenAuthorityPath
	}
	if len(a.ListenAuthorityRaw) < 1 {
		a.ListenAuthorityRaw = b.ListenAuthorityRaw
	}
	if len(a.SendAuthorityPath) < 1 {
		a.SendAuthorityPath = b.SendAuthorityPath
	}
	if len(a.SendAuthorityRaw) < 1 {
		a.SendAuthorityRaw = b.SendAuthorityRaw
	}
	return a
}

func profileSuffix(x, s string) string {
	if strings.HasSuffix(x, s) {
		index := len(x) - len(s)
		return x[index:]
	}
	return ""
}

func (p Profile) Copy() (nu *Profile) {
	nu = new(Profile)
	nu.Name = p.Name
	nu.Listen = p.Listen
	nu.Proxy = p.Proxy
	nu.Protocol = p.Protocol
	nu.ListenCertPath = p.ListenCertPath
	nu.ListenCertRaw = p.ListenCertRaw
	nu.ListenPrivatePath = p.ListenPrivatePath
	nu.ListenPrivateRaw = p.ListenPrivateRaw
	nu.ListenAuthorityPath = p.ListenAuthorityPath
	nu.ListenAuthorityRaw = p.ListenAuthorityRaw
	nu.SendCertPath = p.SendCertPath
	nu.SendCertRaw = p.SendCertRaw
	nu.SendPrivatePath = p.SendPrivatePath
	nu.SendPrivateRaw = p.SendPrivateRaw
	nu.SendAuthorityPath = p.SendAuthorityPath
	nu.SendAuthorityRaw = p.SendAuthorityRaw
	nu.Source = p.Source
	return
}

// resolve will load any files from the filesystem that are pending
func (p *Profile) Resolve() error {
	if len(p.ListenCertRaw) < 1 && len(p.ListenCertPath) > 0 {
		b, err := os.ReadFile(p.ListenCertPath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.ListenCertPath, err)
		}
		p.ListenCertRaw = string(b)
	}
	if len(p.SendCertRaw) < 1 && len(p.SendCertPath) > 0 {
		b, err := os.ReadFile(p.SendCertPath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.SendCertPath, err)
		}
		p.SendCertRaw = string(b)
	}
	if len(p.ListenPrivateRaw) < 1 && len(p.ListenPrivatePath) > 0 {
		b, err := os.ReadFile(p.ListenPrivatePath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.ListenPrivatePath, err)
		}
		p.ListenPrivateRaw = string(b)
	}
	if len(p.SendPrivateRaw) < 1 && len(p.SendPrivatePath) > 0 {
		b, err := os.ReadFile(p.SendPrivatePath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.SendPrivatePath, err)
		}
		p.SendPrivateRaw = string(b)
	}
	if len(p.ListenAuthorityRaw) < 1 && len(p.ListenAuthorityPath) > 0 {
		b, err := os.ReadFile(p.ListenAuthorityPath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.ListenAuthorityPath, err)
		}
		p.ListenAuthorityRaw = string(b)
	}
	if len(p.SendAuthorityRaw) < 1 && len(p.SendAuthorityPath) > 0 {
		b, err := os.ReadFile(p.SendAuthorityPath)
		if err != nil {
			return fmt.Errorf("reading file %q: %w", p.SendAuthorityPath, err)
		}
		p.SendAuthorityRaw = string(b)
	}
	return nil
}

// ListenChanged will compare profiles to see if the listen side of the connection
// needs to be changed.
func (p *Profile) ListenChanged(q *Profile) bool {
	if p.Listen != q.Listen {
		return true
	}
	if p.Protocol != q.Protocol {
		return true
	}
	if p.ListenAuthorityRaw != q.ListenAuthorityRaw {
		return true
	}
	if p.ListenCertRaw != q.ListenCertRaw {
		return true
	}
	if p.ListenPrivateRaw != q.ListenPrivateRaw {
		return true
	}

	return false
}

// DestinationChanged will compare profiles to see if the destination side of the
// connection needs to be changed.
func (p *Profile) DestinationChanged(q *Profile) bool {
	if p.Proxy != q.Proxy {
		return true
	}
	if p.Protocol != q.Protocol {
		return true
	}
	if p.SendAuthorityRaw != q.SendAuthorityRaw {
		return true
	}
	if p.SendCertRaw != q.SendCertRaw {
		return true
	}
	if p.SendPrivateRaw != q.SendPrivateRaw {
		return true
	}

	return false
}
