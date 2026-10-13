package store

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Firestore uses ADC (Cloud Run service identity), separate from relay collections.
type Firestore struct {
	Client     *firestore.Client
	Collection string
}

func NewFirestore(ctx context.Context, project, database, collection string) (*Firestore, error) {
	if project == "" || !validKey.MatchString(collection) || strings.HasPrefix(collection, "relay_") {
		return nil, errors.New("configure a project and a dedicated Commerce Firestore collection")
	}
	if database == "" {
		database = "(default)"
	}
	client, err := firestore.NewClientWithDatabase(ctx, project, database)
	if err != nil {
		return nil, errors.New("cannot initialize Firestore credentials")
	}
	return &Firestore{client, collection}, nil
}
func (f *Firestore) ref(key string) (*firestore.DocumentRef, error) {
	if !validKey.MatchString(key) {
		return nil, ErrNotFound
	}
	collection := f.Collection
	if strings.HasPrefix(key, "rl_") {
		collection += "_limits"
	}
	return f.Client.Collection(collection).Doc(key), nil
}
func payload(doc *firestore.DocumentSnapshot) ([]byte, error) {
	data, ok := doc.Data()["payload"].([]byte)
	if !ok {
		return nil, errors.New("invalid Firestore record")
	}
	return data, nil
}
func (f *Firestore) Get(ctx context.Context, key string) ([]byte, error) {
	ref, err := f.ref(key)
	if err != nil {
		return nil, err
	}
	doc, err := ref.Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, errors.New("Firestore unavailable")
	}
	return payload(doc)
}
func (f *Firestore) CAS(ctx context.Context, key string, expected, next []byte) error {
	ref, err := f.ref(key)
	if err != nil {
		return err
	}
	err = f.Client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		doc, err := tx.Get(ref)
		var current []byte
		if status.Code(err) != codes.NotFound {
			if err != nil {
				return err
			}
			current, err = payload(doc)
			if err != nil {
				return err
			}
		}
		if !bytes.Equal(current, expected) {
			return ErrConflict
		}
		return tx.Set(ref, map[string]any{"payload": next})
	})
	if errors.Is(err, ErrConflict) {
		return ErrConflict
	}
	if err != nil {
		return errors.New("Firestore write unavailable")
	}
	return nil
}
func (f *Firestore) Scan(ctx context.Context, cursor string, limit int) ([]string, string, error) {
	if limit < 1 || limit > 500 {
		return nil, "", errors.New("scan limit must be between 1 and 500")
	}
	q := f.Client.Collection(f.Collection).OrderBy(firestore.DocumentID, firestore.Asc).Limit(limit)
	if cursor != "" {
		if !validKey.MatchString(cursor) {
			return nil, "", errors.New("invalid scan cursor")
		}
		q = q.StartAfter(cursor)
	}
	it := q.Documents(ctx)
	defer it.Stop()
	var keys []string
	for {
		doc, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, "", errors.New("Firestore scan unavailable")
		}
		keys = append(keys, doc.Ref.ID)
	}
	next := ""
	if len(keys) == limit {
		next = keys[len(keys)-1]
	}
	return keys, next, nil
}

func (f *Firestore) Close() error { return f.Client.Close() }
