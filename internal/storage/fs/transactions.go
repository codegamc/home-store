package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// transaction is a durable intent record. It lets startup finish an operation
// that was interrupted between the data and metadata renames. Paths are
// created by this package and are not derived from request input.
type transaction struct {
	Operation string `json:"operation"`
	DataTemp  string `json:"data_temp,omitempty"`
	DataFinal string `json:"data_final"`
	MetaTemp  string `json:"meta_temp,omitempty"`
	MetaFinal string `json:"meta_final"`
}

func (f *FSBackend) transactionPath(bucket, key string) string {
	return filepath.Join(f.transactionsPath(), objectFileName(bucket+"\x00"+key)+".json")
}

func (f *FSBackend) writeTransaction(bucket, key string, tx transaction) (string, error) {
	path := f.transactionPath(bucket, key)
	data, err := json.Marshal(tx)
	if err != nil {
		return "", fmt.Errorf("marshal transaction: %w", err)
	}
	tmp, err := os.CreateTemp(f.transactionsPath(), ".transaction-*")
	if err != nil {
		return "", fmt.Errorf("create transaction: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("set transaction permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write transaction: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("sync transaction: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close transaction: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("commit transaction: %w", err)
	}
	if err := syncDirectory(f.transactionsPath()); err != nil {
		return "", err
	}
	f.pendingTxn = true
	return path, nil
}

func (f *FSBackend) finishPutTransaction(tx transaction) error {
	if tx.DataTemp != "" {
		if _, err := os.Stat(tx.DataTemp); err == nil {
			if err := os.Rename(tx.DataTemp, tx.DataFinal); err != nil {
				return fmt.Errorf("finalize object data: %w", err)
			}
			if err := syncDirectory(filepath.Dir(tx.DataFinal)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect temporary object data: %w", err)
		}
	}
	if tx.MetaTemp != "" {
		if _, err := os.Stat(tx.MetaTemp); err == nil {
			if err := os.Rename(tx.MetaTemp, tx.MetaFinal); err != nil {
				return fmt.Errorf("finalize object metadata: %w", err)
			}
			if err := syncDirectory(filepath.Dir(tx.MetaFinal)); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect temporary object metadata: %w", err)
		}
	}
	if _, err := os.Stat(tx.DataFinal); err != nil {
		return fmt.Errorf("transaction is missing committed object data: %w", err)
	}
	if _, err := os.Stat(tx.MetaFinal); err != nil {
		return fmt.Errorf("transaction is missing committed object metadata: %w", err)
	}
	return nil
}

func (f *FSBackend) finishDeleteTransaction(tx transaction) error {
	for _, path := range []string{tx.DataFinal, tx.MetaFinal} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("finalize object deletion: %w", err)
		}
	}
	if err := syncDirectory(filepath.Dir(tx.DataFinal)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(tx.MetaFinal))
}

func (f *FSBackend) completeTransaction(path string, tx transaction) error {
	var err error
	switch tx.Operation {
	case "put":
		err = f.finishPutTransaction(tx)
	case "delete":
		err = f.finishDeleteTransaction(tx)
	default:
		return fmt.Errorf("unknown transaction operation %q", tx.Operation)
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed transaction: %w", err)
	}
	if err := syncDirectory(f.transactionsPath()); err != nil {
		return err
	}
	f.pendingTxn = false
	return nil
}

func (f *FSBackend) recoverTransactions() error {
	entries, err := os.ReadDir(f.transactionsPath())
	if err != nil {
		return fmt.Errorf("read transaction directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(f.transactionsPath(), entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read transaction %s: %w", entry.Name(), err)
		}
		var tx transaction
		if err := json.Unmarshal(data, &tx); err != nil {
			return fmt.Errorf("decode transaction %s: %w", entry.Name(), err)
		}
		if tx.DataFinal == "" || tx.MetaFinal == "" {
			return fmt.Errorf("transaction %s is incomplete", entry.Name())
		}
		if err := f.completeTransaction(path, tx); err != nil {
			return fmt.Errorf("recover transaction %s: %w", entry.Name(), err)
		}
	}
	f.pendingTxn = false
	return nil
}

func (f *FSBackend) recoverIfNeeded() error {
	if !f.pendingTxn {
		return nil
	}
	return f.recoverTransactions()
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
