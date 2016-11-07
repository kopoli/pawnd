package pawnd

import (
	"fmt"

	util "github.com/kopoli/go-util"
	"gopkg.in/ini.v1"
)

type Config struct {
	Name     string
	Pattern  string
	Exec     string
	IsDaemon bool
}

func getDefKey(sect *ini.Section, name string, defvalue string) (key *ini.Key) {

	key, err := sect.GetKey(name)
	if err != nil {
		key, err = sect.NewKey(name, defvalue)
		if err != nil {
			fmt.Println("Could not create new key to section", sect.Name(), "Key:", name)
		}
	}

	return
}

func LoadConfigs(opts util.Options) (ret []Config, err error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{AllowBooleanKeys: true},
		opts.Get("configuration-file", "pawnd.conf"))
	if err != nil {
		err = util.E.Annotate(err, "Could not load configuration file")
		return
	}

	sects := cfg.Sections()
	ret = make([]Config, len(sects))
	for i, sect := range sects {
		ret[i] = Config{
			Name:     sect.Name(),
			Pattern:  getDefKey(sect, "pattern", "").String(),
			Exec:     getDefKey(sect, "exec", "").String(),
			IsDaemon: getDefKey(sect, "daemon", "false").MustBool(false),
		}
	}

	return
}

// type Config interface {
// 	// Load(name string) error
// 	Save(name string) error
// 	Get(name string, defvalue string) string
// }

type cfg struct {
	Cfg ini.File
}

// func LoadConfig(name string) (ret Config, err error) {

// 	return
// }
