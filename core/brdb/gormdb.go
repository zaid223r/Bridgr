package brdb

import (
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ConnectPostgres(user, pass, host, dbname string, port int) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%d sslmode=disable",
		host, user, pass, dbname, port)
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}