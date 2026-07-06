// storage = ouverture de la base + migrations
package storage

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mmarquet/native-api/internal/domain"
)

// open la base sqlite (fichier) + applique les migrations
func Open(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&domain.Project{},
		&domain.Service{},
		&domain.Server{},
		&domain.Deployment{},
		&domain.Container{},
	); err != nil {
		return nil, err
	}
	return db, nil
}
