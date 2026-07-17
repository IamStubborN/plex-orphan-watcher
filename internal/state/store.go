package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/IamStubborN/plex-orphan-watcher/internal/model"
	bolt "go.etcd.io/bbolt"
)

var (
	activeBucket  = []byte("active")
	pendingBucket = []byte("pending")
	metaBucket    = []byte("meta")
	lastSyncKey   = []byte("last_sync")
)

type Store struct {
	db *bolt.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	store := &Store{db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{activeBucket, pendingBucket, metaBucket} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize state database: %w", err)
	}
	return store, nil
}

func OpenReadOnly(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true, Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open state database read-only: %w", err)
	}
	store := &Store{db: db}
	if err := db.View(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{activeBucket, pendingBucket, metaBucket} {
			if tx.Bucket(name) == nil {
				return fmt.Errorf("state bucket %q is missing", name)
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) Reconcile(snapshot model.Snapshot, observedAt time.Time, settleDelay time.Duration) ([]model.Pending, error) {
	var created []model.Pending
	err := store.db.Update(func(tx *bolt.Tx) error {
		active := tx.Bucket(activeBucket)
		pending := tx.Bucket(pendingBucket)
		if err := active.ForEach(func(key, value []byte) error {
			if _, exists := snapshot.Items[string(key)]; exists {
				return pending.Delete(key)
			}
			if pending.Get(key) != nil {
				return nil
			}
			var item model.Item
			if err := json.Unmarshal(value, &item); err != nil {
				return fmt.Errorf("decode active item %q: %w", key, err)
			}
			candidate := model.Pending{
				ID: string(key), Item: item, Trigger: "reconcile",
				ObservedAt: observedAt, DueAt: observedAt.Add(settleDelay), Status: model.PendingWaiting,
			}
			encoded, err := json.Marshal(candidate)
			if err != nil {
				return err
			}
			if err := pending.Put(key, encoded); err != nil {
				return err
			}
			created = append(created, candidate)
			return nil
		}); err != nil {
			return err
		}

		var keys [][]byte
		if err := active.ForEach(func(key, _ []byte) error {
			keys = append(keys, append([]byte(nil), key...))
			return nil
		}); err != nil {
			return err
		}
		for _, key := range keys {
			if err := active.Delete(key); err != nil {
				return err
			}
		}
		for key, item := range snapshot.Items {
			encoded, err := json.Marshal(item)
			if err != nil {
				return err
			}
			if err := active.Put([]byte(key), encoded); err != nil {
				return err
			}
			if err := pending.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return tx.Bucket(metaBucket).Put(lastSyncKey, []byte(snapshot.SyncedAt.UTC().Format(time.RFC3339Nano)))
	})
	return created, err
}

func (store *Store) Enqueue(ratingKey, trigger string, observedAt, dueAt time.Time) (model.Pending, error) {
	var result model.Pending
	err := store.db.Update(func(tx *bolt.Tx) error {
		pending := tx.Bucket(pendingBucket)
		if value := pending.Get([]byte(ratingKey)); value != nil {
			return json.Unmarshal(value, &result)
		}
		value := tx.Bucket(activeBucket).Get([]byte(ratingKey))
		if value == nil {
			return fmt.Errorf("rating key %q is not present in inventory", ratingKey)
		}
		var item model.Item
		if err := json.Unmarshal(value, &item); err != nil {
			return err
		}
		result = model.Pending{ID: ratingKey, Item: item, Trigger: trigger, ObservedAt: observedAt, DueAt: dueAt, Status: model.PendingWaiting}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return pending.Put([]byte(ratingKey), encoded)
	})
	return result, err
}

func (store *Store) Pending(includeDryRun bool) ([]model.Pending, error) {
	var result []model.Pending
	err := store.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(pendingBucket).ForEach(func(_, value []byte) error {
			var pending model.Pending
			if err := json.Unmarshal(value, &pending); err != nil {
				return err
			}
			if includeDryRun || pending.Status != model.PendingDryRun {
				result = append(result, pending)
			}
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ObservedAt.Before(result[j].ObservedAt) })
	return result, err
}

func (store *Store) MarkDryRun(id string, plan model.DeletionPlan) error {
	return store.updatePending(id, func(pending *model.Pending) {
		pending.Status = model.PendingDryRun
		pending.Plan = plan
	})
}

func (store *Store) ResetWaiting(id string) error {
	return store.updatePending(id, func(pending *model.Pending) {
		pending.Status = model.PendingWaiting
		pending.Plan = model.DeletionPlan{}
	})
}

func (store *Store) Complete(id string) error {
	return store.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(pendingBucket).Delete([]byte(id)) })
}

func (store *Store) LastSync() (time.Time, error) {
	var result time.Time
	err := store.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(metaBucket).Get(lastSyncKey)
		if value == nil {
			return errors.New("inventory has not been synchronized")
		}
		parsed, err := time.Parse(time.RFC3339Nano, string(value))
		if err != nil {
			return err
		}
		result = parsed
		return nil
	})
	return result, err
}

func (store *Store) updatePending(id string, update func(*model.Pending)) error {
	return store.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pendingBucket)
		value := bucket.Get([]byte(id))
		if value == nil {
			return fmt.Errorf("pending item %q not found", id)
		}
		var pending model.Pending
		if err := json.Unmarshal(value, &pending); err != nil {
			return err
		}
		update(&pending)
		encoded, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(id), encoded)
	})
}
