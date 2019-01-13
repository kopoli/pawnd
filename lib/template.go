package pawnd

import (
	"fmt"
	"io"
	"os"
	"strings"

	util "github.com/kopoli/go-util"
)

func Templates() map[string]string {
	return map[string]string{
		"godoc": `[godoc]
init
daemon=godoc -http=:6060
`,
		"gobuild": `[gobuild]
init
exec=go build
`,
		"gotest": `[gotest]
init
exec=go test
`,
	}
}

func GenerateTemplates(opts util.Options) error {
	templates := strings.Split(opts.Get("generate-templates", ""), " ")

	var out io.Writer
	if opts.IsSet("generate-stdout") {
		out = os.Stdout
	} else {
		filename := opts.Get("generate-configuration-file", "Pawnfile")
		overwrite := opts.IsSet("generate-overwrite")
		if _, err := os.Stat(filename); err == nil && !overwrite {
			return fmt.Errorf("File %s already exists", filename)
		}

		fp, err := os.Create(filename)
		if err != nil {
			return util.E.Annotate(err, "Could not create file", filename)
		}
		defer fp.Close()
		out = fp
	}
	all := Templates()

	for i := range templates {
		if _, ok := all[templates[i]]; !ok {
			return fmt.Errorf("Invalid template %s", templates[i])
		}
	}

	for i := range templates {
		_, err := out.Write([]byte(all[templates[i]]))
		if err == nil {
			_, err = out.Write([]byte{'\n'})
		}
		if err != nil {
			return util.E.Annotate(err, "Could not write template")
		}
	}

	return nil
}
