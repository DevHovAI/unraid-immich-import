// fakeimmich startet das nachgebaute Immich für Rauchtests des Containers:
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
	s.Close() // nur den Handler verwenden, auf festem Port
	go func() {
		for range time.Tick(2 * time.Second) {
			s.Lock()
			fmt.Printf("fakeimmich: %d Dateien, %d Uploads, Alben %v, %d Stapel\n",
				len(s.Assets), s.Uploads, s.Albums, len(s.Stacks))
			s.Unlock()
		}
	}()
	fmt.Println("fakeimmich auf", addr, "API-Key KEY")
	panic(http.ListenAndServe(addr, s.Config.Handler))
}
