package database

import (
	"sync"

	"gorm.io/gorm"
)

var (
	db    *gorm.DB
	mutex sync.RWMutex
)

func SetDatabase(d *gorm.DB) {
	db = d
}

func ReadDB[T any](callback func(*gorm.DB) (T, error)) (T, error) {
	mutex.RLock()
	defer mutex.RUnlock()
	return callback(db)
}

func WriteDB[T any](callback func(*gorm.DB) (T, error)) (T, error) {
	mutex.Lock()
	defer mutex.Unlock()
	return callback(db)
}
