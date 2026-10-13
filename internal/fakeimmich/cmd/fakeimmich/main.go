// fakeimmich starts the fake Immich for smoke tests of the container:
//
//	go run ./internal/fakeimmich/cmd/fakeimmich :2283
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/DevHovAI/unraid-immich-import/internal/fakeimmich"
)

func main() {
	addr := ":2283"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	s := fakeimmich.New()
	s.Close() // use only the handler, on a fixed port
	go func() {
		for range time.Tick(2 * time.Second) {
			s.Lock()
			fmt.Printf("fakeimmich: %d files, %d uploads, albums %v, %d stacks\n",
				len(s.Assets), s.Uploads, s.Albums, len(s.Stacks))
			s.Unlock()
		}
	}()
	fmt.Println("fakeimmich on", addr, "API key KEY")
	panic(http.ListenAndServe(addr, s.Config.Handler))
}
