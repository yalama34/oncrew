package postgres

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBName     string
}

func GetenvWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func Load() (Config, error) {
	if err := godotenv.Overload(); err != nil {
		_ = godotenv.Overload("../.env")
	}

	dbUser := GetenvWithDefault("DB_USER", "postgres")
	dbPassword := GetenvWithDefault("DB_PASSWORD", "postgres")
	dbHost := GetenvWithDefault("DB_HOST", "localhost")
	dbPort := GetenvWithDefault("DB_PORT", "5432")
	dbName := GetenvWithDefault("POSTGRES_DB", "orchestrator")

	return Config{
		DBUser:     dbUser,
		DBPassword: dbPassword,
		DBHost:     dbHost,
		DBPort:     dbPort,
		DBName:     dbName,
	}, nil
}

func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		c.DBHost,
		c.DBUser,
		c.DBPassword,
		c.DBName,
		c.DBPort,
	)
}
