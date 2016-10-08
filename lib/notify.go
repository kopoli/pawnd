package pawnd

import (
	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	watch fsnotify.Watcher
}
