package cmd

import (
	"fmt"
	"os"

	"github.com/gregtuc/memsync/internal/crypto"
	"github.com/gregtuc/memsync/internal/paths"
	"github.com/gregtuc/memsync/internal/vault"
)

func loadOrCreateSetupKey() ([]byte, bool, error) {
	key, err := crypto.LoadKey(paths.KeyPath())
	if err == nil {
		return key, false, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}
	recovery, checkErr := keyRecoveryRequired()
	if checkErr != nil {
		return nil, false, checkErr
	}
	if recovery {
		return nil, false, missingEstablishedKeyError()
	}
	return crypto.LoadOrCreateKey(paths.KeyPath())
}

func validateSetupKeyState() error {
	if _, err := crypto.LoadKey(paths.KeyPath()); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	recovery, err := keyRecoveryRequired()
	if err != nil {
		return err
	}
	if recovery {
		return missingEstablishedKeyError()
	}
	return nil
}

func keyRecoveryRequired() (bool, error) {
	if vault.RemoteURL() != "" {
		return true, nil
	}
	if records, err := vault.Records(); err == nil {
		if len(records) > 0 {
			return true, nil
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	return vault.HasEncryptedHistory()
}

func missingEstablishedKeyError() error {
	return fmt.Errorf("the encryption key is missing but this vault already contains shared history; refusing to create an incompatible replacement. Restore the key from a paired laptop/backup, or purge this local setup and pair it again")
}
