package pawnd

import "github.com/mattn/go-zglob"

type Trigger struct{}

type TriggerHandler struct {

}

func TriggerOnFileChanges(patterns []string, t chan<- Trigger) (th TriggerHandler, err error) {
	return
}

func (h *TriggerHandler) Close() (err error) {
	return
}

//// 

func Glob(pattern string) (matches []string, err error) {
	return zglob.Glob(pattern)
}
