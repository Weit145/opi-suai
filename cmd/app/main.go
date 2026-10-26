package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Weit145/opi-suai/internal/app"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if e := run(); e != nil {
		slog.Error("application stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	seed := flag.Bool("seed", false, "Load explicit demonstration data")
	migrate := flag.Bool("migrate", false, "Migrate then exit")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	var c app.Config
	if e := cleanenv.ReadEnv(&c); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pc, e := pgxpool.ParseConfig(c.DatabaseURL)
	if e != nil {
		return e
	}
	pc.MaxConns = 20
	db, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		return e
	}
	s := app.New(db, c)
	if e = s.Initialize(ctx, *seed, os.Getenv("DEMO_PASSWORD")); e != nil {
		return e
	}
	if *migrate {
		return nil
	}
	go s.RunWorker(ctx)
	server := &http.Server{Addr: c.Addr, Handler: s.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("listening", "address", c.Addr, "demo", c.Demo)
	e = server.ListenAndServe()
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}
