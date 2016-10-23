package pawnd

import (
	"fmt"
	"path"
	"path/filepath"
	"sync"
	"time"

	util "github.com/kopoli/go-util"
	"github.com/mattn/go-zglob"
	fsnotify "gopkg.in/fsnotify.v1"
)

type Trigger struct{}

type TriggerHandler struct {
	done chan bool
}

// Remove duplicate strings from the list
func uniqStr(in []string) (out []string) {
	set := make(map[string]bool, len(in))

	for _, item := range in {
		set[item] = true
	}
	out = make([]string, len(set))
	for item := range set {
		out = append(out, item)
		fmt.Println("item:[", item, "]")
	}
	return
}

// Get the list of files represented by the given list of glob patterns
func getFileList(patterns []string) (ret []string) {
	for _, pattern := range patterns {
		// Recursive globbing support
		m, err := zglob.Glob(pattern)
		if err != nil {
			continue
		}

		ret = append(ret, m...)
	}

	for _, path := range ret {
		ret = append(ret, filepath.Dir(path))
	}

	ret = uniqStr(ret)

	return
}

func TriggerOnFileChanges(patterns []string, t chan<- Trigger) (th TriggerHandler, err error) {

	files := getFileList(patterns)
	if len(files) == 0 {
		err = util.E.New("No watched files found")
		return
	}

	fmt.Println("Files", files)

	// Create a watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		err = util.E.Annotate(err, "Creating a file watcher failed")
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)

	stopTimer := func(t *time.Timer) {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
	}

	// Match non-recursive parts of the patterns against the given file
	matchPattern := func(file string) bool {
		file = filepath.Base(file)
		for _, p := range patterns {
			m, er := path.Match(filepath.Base(p), file)
			if m && er == nil {
				return true
			}
		}
		return false
	}

	go func() {
		defer watch.Close()

		threshold := time.NewTimer(0)
		stopTimer(threshold)

		for _, name := range files {
			err = watch.Add(name)
			if err != nil {
				fmt.Println("Could not watch", name)
			}
		}

		for {
			select {
			case <-threshold.C:
				fmt.Println("Would send an event")
				t <- Trigger{}
			case event := <-watch.Events:
				fmt.Println("Event received:", event)
				if matchPattern(event.Name) {
					fmt.Println("Pattern matched.")
					stopTimer(threshold)
					threshold.Reset(time.Millisecond * 500)
				}

			case err := <-watch.Errors:
				fmt.Println("Error received", err)
			}
		}
	}()

	wg.Wait()

	return
}

func (h *TriggerHandler) Close() (err error) {
	return
}
