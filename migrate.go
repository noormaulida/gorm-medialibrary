package medialibrary

import "gorm.io/gorm"

// Migrate creates the Spatie-compatible media table when missing.
// Skip this when the database already uses Laravel Media Library migrations.
func Migrate(db *gorm.DB) error {
	if db == nil {
		return ErrInvalidArgument
	}
	return db.AutoMigrate(&Media{})
}
