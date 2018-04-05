package pawnd

import (
	"fmt"
	"strings"
	"unicode"

	util "github.com/kopoli/go-util"
	"gopkg.in/ini.v1"
)

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
			err = fmt.Errorf("No child sections supported")
			goto fail
		}

		count := 0
		types := []string{"daemon", "exec", "file"}
		for i := range types {
			if sect.HasKey(types[i]) {
				count++
			}
		}

		if count != 1 {
			err = fmt.Errorf("Each section should have exactly one of: %s", strings.Join(types, ", "))
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
				bus.Register(fmt.Sprintf("init:%s", ActionName(sect.Name())), a)
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
