// Command boardgame-server runs an HTTP/WebSocket server hosting the
// built-in reference games (tic-tac-toe, Love Letter, and rock-paper-scissors).
// It's the MVP entry point — adapt freely when adding more games.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/tjcran/boardgame-go/games/loveletter"
	"github.com/tjcran/boardgame-go/games/rps"
	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/server"
	"github.com/tjcran/boardgame-go/storage"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	certFile := flag.String("cert", "", "TLS certificate (PEM); enables HTTPS")
	keyFile := flag.String("key", "", "TLS private key (PEM)")
	flag.Parse()

	m := match.NewManager(storage.NewMemory())
	m.Register(tictactoe.New())
	m.Register(loveletter.New())
	m.Register(rps.New())

	srv := server.New(m)
	srv.Origins = []string{server.OriginLocalhostInDevelopment}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("boardgame-server listening on %s", *addr)
	var err error
	if *certFile != "" || *keyFile != "" {
		err = srv.RunTLS(ctx, *addr, *certFile, *keyFile)
	} else {
		err = srv.Run(ctx, *addr)
	}
	if err != nil {
		log.Fatal(err)
	}
}
