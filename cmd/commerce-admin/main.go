// Commerce administration defaults to preview and does not call payment APIs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/artheras/aria-plugins/packages/commerce"
	"github.com/artheras/aria-plugins/packages/identity"
	"github.com/artheras/aria-plugins/packages/store"
)

func run(args []string) error {
	if len(args) == 1 && args[0] == "legacy-owner" {
		secret, token := os.Getenv("ARIA_SESSION_SECRET"), os.Getenv("ARIA_MERCHANT_TOKEN")
		if len(secret) < 32 || len(token) < 32 {
			return errors.New("set the original session secret and merchant token locally")
		}
		fmt.Println(identity.LegacyOwner(secret, token))
		return nil
	}
	if len(args) == 0 || args[0] != "migrate-owner" {
		return errors.New("usage: aria-commerce-admin legacy-owner | migrate-owner --from-owner HASH --merchant-id ID [--apply]")
	}
	f := flag.NewFlagSet("migrate-owner", flag.ContinueOnError)
	kind := f.String("store", "file", "file or firestore")
	directory := f.String("data-dir", "data", "single-process data directory")
	project := f.String("project", "", "Google Cloud project")
	database := f.String("database", "(default)", "dedicated database ID")
	collection := f.String("collection", "aria_commerce_records", "Commerce collection")
	from := f.String("from-owner", "", "original 64-character legacy owner hash")
	merchant := f.String("merchant-id", "", "new stable merchant ID")
	apply := f.Bool("apply", false, "apply compare-and-swap ownership migration")
	cursor := f.String("cursor", "", "resume scan cursor")
	pages := f.Int("pages", 100, "maximum pages, each containing 500 records")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if !identity.ValidateLegacyOwner(*from) {
		return errors.New("provide a valid legacy owner hash")
	}
	if err := identity.ValidateID(*merchant); err != nil {
		return err
	}
	if *pages < 1 || *pages > 10000 || f.NArg() != 0 {
		return errors.New("invalid migration bounds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	var storage store.Store
	var err error
	switch *kind {
	case "file":
		if os.Getenv("K_SERVICE") != "" || os.Getenv("CLOUD_RUN_JOB") != "" {
			return errors.New("Cloud Run administration requires shared Firestore storage")
		}
		storage, err = store.NewFile(*directory)
	case "firestore":
		storage, err = store.NewFirestore(ctx, *project, *database, *collection)
	default:
		return errors.New("store must be file or firestore")
	}
	if err != nil {
		return err
	}
	if closer, ok := storage.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	total := 0
	for range *pages {
		next, count, err := commerce.MigrateOwner(ctx, storage, *from, identity.Owner(*merchant), *cursor, 500, *apply)
		total += count
		if err != nil {
			return err
		}
		*cursor = next
		if next == "" {
			break
		}
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"applied": *apply, "matched_orders": total, "merchant_id": *merchant, "next_cursor": *cursor, "complete": *cursor == ""})
	if *cursor != "" {
		return errors.New("migration scan limit reached; resume using next_cursor")
	}
	return nil
}
func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
