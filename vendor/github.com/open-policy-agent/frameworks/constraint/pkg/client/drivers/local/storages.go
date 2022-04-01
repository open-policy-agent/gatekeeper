package local

import (
	"context"
	"fmt"
	"sync"

	clienterrors "github.com/open-policy-agent/frameworks/constraint/pkg/client/errors"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/storage/inmem"
)

type storages struct {
	mtx sync.RWMutex

	// storage is a map from target name to the Rego data store for Constraints
	// and objects used in referential Constraints.
	// storage internally uses mutexes to guard reads and writes during
	// transactions and queries, so we don't need to explicitly guard individual
	// Stores with mutexes.
	storage map[string]storage.Store
}

func (d *storages) addData(ctx context.Context, target string, path storage.Path, data interface{}) error {
	store, err := d.getStorage(ctx, target)
	if err != nil {
		return err
	}

	return addData(ctx, store, path, data)
}

func (d *storages) removeData(ctx context.Context, target string, path storage.Path) error {
	store, err := d.getStorage(ctx, target)
	if err != nil {
		return err
	}

	return removeData(ctx, store, path)
}

func (d *storages) removeDataEach(ctx context.Context, path storage.Path) error {
	d.mtx.RLock()
	defer d.mtx.RUnlock()

	for _, store := range d.storage {
		err := removeData(ctx, store, path)
		if err != nil {
			return err
		}
	}

	return nil
}

// getStorage gets the Rego Store for a target, or instantiates it if it does not
// already exist.
// Instantiates data.inventory for the store.
func (d *storages) getStorage(ctx context.Context, target string) (storage.Store, error) {
	// Fast path only acquires a read lock to retrieve storage if it already exists.
	d.mtx.RLock()
	store, found := d.storage[target]
	d.mtx.RUnlock()
	if found {
		return store, nil
	}

	d.mtx.Lock()
	defer d.mtx.Unlock()
	store, found = d.storage[target]
	if found {
		// Exit fast if the storage has been created since we last checked.
		return store, nil
	}

	// We know that storage doesn't exist yet, and have a lock so we know no other
	// threads will attempt to create it.
	store = inmem.New()
	d.storage[target] = store

	txn, err := store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	path := inventoryPath(nil)

	err = storage.MakeDir(ctx, store, txn, path)
	if err != nil {
		store.Abort(ctx, txn)
		return nil, fmt.Errorf("%v: unable to make directory for target %q %v",
			clienterrors.ErrWrite, target, err)
	}

	err = store.Commit(ctx, txn)
	if err != nil {
		// inmem.Store automatically aborts the transaction for us.
		return nil, fmt.Errorf("%v: unable to make directory for target %q %v",
			clienterrors.ErrWrite, target, err)
	}

	return store, nil
}

func inventoryPath(path []string) storage.Path {
	return append([]string{"external"}, path...)
}

func addData(ctx context.Context, store storage.Store, path storage.Path, data interface{}) error {
	if len(path) == 0 {
		// Sanity-check path.
		// This would overwrite "data", erasing all Constraints and stored objects.
		return fmt.Errorf("%w: path must contain at least one path element: %+v", clienterrors.ErrPathInvalid, path)
	}

	// Initiate a new transaction. Since this is a write-transaction, it blocks
	// all other reads and writes, which includes running queries. If a transaction
	// is successfully created, all code paths must either Abort or Commit the
	// transaction to unblock queries and other writes.
	txn, err := store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	// We can't write to a location if its parent doesn't exist.
	// Thus, we check to see if anything already exists at the path.
	_, err = store.Read(ctx, txn, path)
	if storage.IsNotFound(err) {
		// Insert an empty object at the path's parent so its parents are
		// recursively created.
		parent := path[:len(path)-1]
		err = storage.MakeDir(ctx, store, txn, parent)
		if err != nil {
			store.Abort(ctx, txn)
			return fmt.Errorf("%w: unable to make directory: %v", clienterrors.ErrWrite, err)
		}
	} else if err != nil {
		// We weren't able to read from storage - something serious is likely wrong.
		store.Abort(ctx, txn)
		return fmt.Errorf("%w: %v", clienterrors.ErrRead, err)
	}

	err = store.Write(ctx, txn, storage.AddOp, path, data)
	if err != nil {
		store.Abort(ctx, txn)
		return fmt.Errorf("%w: unable to write data: %v", clienterrors.ErrWrite, err)
	}

	err = store.Commit(ctx, txn)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	return nil
}

func removeData(ctx context.Context, store storage.Store, path storage.Path) error {
	txn, err := store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	err = store.Write(ctx, txn, storage.RemoveOp, path, interface{}(nil))
	if err != nil {
		store.Abort(ctx, txn)
		if storage.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("%w: unable to remove data: %v", clienterrors.ErrWrite, err)
	}

	err = store.Commit(ctx, txn)
	if err != nil {
		return fmt.Errorf("%w: %v", clienterrors.ErrTransaction, err)
	}

	return nil
}
