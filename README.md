# vnstat-in-go

This is a simple command line observing library for vnstat written in Go.

## Installation

```bash
go get github.com/snowmerak/vnstat-in-go
```

## Usage

```go
package main

import (
	"context"
	"github.com/snowmerak/vnstat-in-go"
	"log"
	"os"
	"os/signal"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	w := vnstat.NewWatcher(ctx, vnstat.UnitMbit, 128, nil)
	if err := w.Watch(ctx, "en0", func(t vnstat.Traffic) {
		log.Printf("Rx: %f, Tx: %f", t.Rx, t.Tx)
	}); err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
}
```
