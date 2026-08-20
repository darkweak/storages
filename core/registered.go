package core

import (
	"fmt"
	"sync"

	"github.com/pierrec/lz4/v4"
)

// Lz4WriterPool pools lz4 writers, which are safe to reuse once Close has
// flushed the frame. Callers must Reset the writer before use and only
// return it to the pool after Close. Readers must never be pooled this way:
// a pooled reader escapes through http.Response.Body and would be recycled
// while another goroutine still reads from it.
var Lz4WriterPool = sync.Pool{New: func() any { return lz4.NewWriter(nil) }}

// MappingWalker is an optional interface a Storer can implement to stream
// mapping entries in bounded batches instead of materializing the whole
// mapping index in memory like MapKeys does. The walk stops early when fn
// returns false. The key passed to fn is stripped of the given prefix.
type MappingWalker interface {
	WalkMappings(prefix string, fn func(key string, value []byte) bool) error
}

var registered = sync.Map{}

func RegisterStorage(s Storer) {
	_ = s.Init()
	registered.Store(fmt.Sprintf("%s-%s", s.Name(), s.Uuid()), s)
}

func GetRegisteredStorer(name string) Storer {
	s, _ := registered.Load(name)
	if s != nil {
		if v, ok := s.(Storer); ok {
			return v
		}
	}

	return nil
}

func ResetRegisteredStorages() {
	registered.Range(func(key, value any) bool {
		registered.Delete(key)

		return true
	})

	registered = sync.Map{}
}

func GetRegisteredStorers() []Storer {
	storers := make([]Storer, 0)

	registered.Range(func(_, value any) bool {
		if s, ok := value.(Storer); ok {
			storers = append(storers, s)
		}

		return true
	})

	return storers
}
