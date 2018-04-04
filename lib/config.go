package pawnd

import (
	"fmt"
	"strings"
	"unicode"

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

// splitWsQuote splits a string by whitespace, but takes doublequotes into
// account
func splitWsQuote(s string) []string {

	quote := rune(0)

	return strings.FieldsFunc(s, func(r rune) bool {
		switch {
		case r == quote:
			quote = rune(0)
			return true
		case quote != rune(0):
			return false
		case unicode.In(r, unicode.Quotation_Mark):
			quote = r
			return true
		default:
			return unicode.IsSpace(r)
		}
	})
}

func ValidateConfig(filename string) (*ini.File, error) {
	fp, err := ini.LoadSources(ini.LoadOptions{
		AllowBooleanKeys: true,
		AllowShadows:     true,
	}, filename)
	if err != nil {
		err = util.E.Annotate(err, "Could not load configuration")
		return nil, err
	}

	for _, sect := range fp.Sections() {
		if sect.Name() == ini.DEFAULT_SECTION {
			continue
		}

		if len(sect.ChildSections()) != 0 {
			err = util.E.New("No child sections supported")
			goto fail
		}
	}

	return fp, nil

fail:
	return nil, err
}

func CreateActions(file *ini.File, bus *EventBus) error {

	for _, sect := range file.Sections() {
		if sect.Name() == ini.DEFAULT_SECTION {
			k, err := sect.GetKey("init")
			if err == nil {
				for _, v := range k.ValueWithShadows() {
					a := NewInitAction(ActionName(v))
					bus.Register(fmt.Sprintf("init:%s", ActionName(v)), a)
				}
			}
			continue
		}
		if sect.HasKey("exec") || sect.HasKey("daemon") {
			keyname := "exec"
			daemon := false
			if sect.HasKey("daemon") {
				daemon = true
				keyname = "daemon"
			}
			key := sect.Key(keyname)
			a := NewExecAction(splitWsQuote(key.String())...)
			a.Daemon = daemon
			a.Succeeded = sect.Key("succeeded").String()
			a.Failed = sect.Key("failed").String()

			bus.Register(ActionName(sect.Name()), a)

			if sect.HasKey("init") {
				a := NewInitAction(ActionName(sect.Name()))
				bus.Register(fmt.Sprintf("init:%d", ActionName(sect.Name())), a)
			}
		} else if sect.HasKey("file") {
			key := sect.Key("file")
			a, err := NewFileAction(splitWsQuote(key.String())...)
			if err == nil {
				a.Changed = sect.Key("changed").String()
				bus.Register(ActionName(sect.Name()), a)
			}
		}
	}

	return nil
}
