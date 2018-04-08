package pawnd

import (
	"fmt"
	"strings"
	"time"
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
		IgnoreInlineComment: true,
		AllowBooleanKeys: true,
	}, filename)
	if err != nil {
		err = util.E.Annotate(err, "Could not load configuration")
		return nil, err
	}

	hasProperDuration := func(sect *ini.Section, key string) error {
		if sect.HasKey(key) {
			var d time.Duration
			var zero time.Duration
			d, err = sect.Key(key).Duration()
			if err != nil || d < zero {

				err = fmt.Errorf("Section \"%s\": Given %s should be a non-negative Duration",
					sect.Name(), key)
				return err
			}
		}
		return nil
	}

	for _, sect := range fp.Sections() {
		if sect.Name() == ini.DEFAULT_SECTION {
			continue
		}

		if len(sect.ChildSections()) != 0 {
			err = fmt.Errorf("Section \"%s\": No child sections supported",
				sect.Name())
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
			err = fmt.Errorf("Section \"%s\": A secion should have exactly one of: %s",
				sect.Name(),
				strings.Join(types, ", "))
			goto fail
		}

		if sect.HasKey("file") {
			err = hasProperDuration(sect, "hysteresis")
			if err != nil {
				goto fail
			}
		}
		if sect.HasKey("exec") || sect.HasKey("daemon") {
			err = hasProperDuration(sect, "cooldown")
			if err != nil {
				goto fail
			}
		}
	}

	return fp, nil

fail:
	return nil, err
}

func CreateActions(file *ini.File, bus *EventBus) error {
	for _, sect := range file.Sections() {
		if sect.HasKey("exec") || sect.HasKey("daemon") {
			keyname := "exec"
			daemon := false
			if sect.HasKey("daemon") {
				daemon = true
				keyname = "daemon"
			}
			key := sect.Key(keyname)
			a := NewExecAction(splitWsQuote(key.String())...)
			a.Cooldown = sect.Key("cooldown").MustDuration(a.Cooldown)
			a.Daemon = daemon
			a.Succeeded = splitWsQuote(sect.Key("succeeded").Value())
			a.Failed = splitWsQuote(sect.Key("failed").Value())

			bus.Register(ActionName(sect.Name()), a)

			if sect.HasKey("init") {
				a := NewInitAction(ActionName(sect.Name()))
				bus.Register(fmt.Sprintf("init:%s", ActionName(sect.Name())), a)
			}
		} else if sect.HasKey("file") {
			key := sect.Key("file")
			a, err := NewFileAction(splitWsQuote(key.String())...)
			if err == nil {
				a.Changed = splitWsQuote(sect.Key("changed").Value())
				a.Hysteresis = sect.Key("hysteresis").MustDuration(a.Hysteresis)
				bus.Register(ActionName(sect.Name()), a)
			}
		}
	}

	return nil
}
