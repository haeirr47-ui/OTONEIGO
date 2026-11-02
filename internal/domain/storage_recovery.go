package domain

// StorageRecoveryStatus exposes counts only, never quarantined user metadata.
type StorageRecoveryStatus struct {
	QuarantinedFavorites int64 `json:"quarantinedFavorites"`
	QuarantinedHistory   int64 `json:"quarantinedHistory"`
	SettingsRecovered    bool  `json:"settingsRecovered"`
}
