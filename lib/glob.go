package pawnd

import "github.com/mattn/go-zglob"


func Glob(pattern string) (matches []string, err error) {
	return zglob.Glob(pattern)
}
