package storage

import (
	"database/sql"
	"errors"

	"github.com/voici5986/otoneigo/internal/domain"
)

func migrateDesktopSettings(tx *sql.Tx) error {
	var data string
	err := tx.QueryRow("SELECT metadata FROM local_settings WHERE id=1").Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var settings domain.LocalSettings
		if err = decodeLocalMetadata(data, &settings); err != nil {
			return err
		}
		settings.Desktop = domain.NormalizeDesktopSettings(settings.Desktop)
		if err = domain.ValidateSettings(settings); err != nil {
			return err
		}
		encoded, err := mergeSettingsMetadata(data, settings)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE local_settings SET metadata=? WHERE id=1", encoded); err != nil {
			return err
		}
	}
	_, err = tx.Exec("PRAGMA user_version=6")
	return err
}
