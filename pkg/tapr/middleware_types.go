package tapr

// Middleware describe middleware config.
type Middleware struct {
	Postgres *PostgresConfig `yaml:"postgres,omitempty"`
	Redis    *RedisConfig    `yaml:"redis,omitempty"`
	MongoDB  *MongodbConfig  `yaml:"mongodb,omitempty"`
}

// Database specify database name and if distributed.
type Database struct {
	Name        string   `yaml:"name" json:"name"`
	Extensions  []string `yaml:"extensions,omitempty" json:"extensions,omitempty"`
	Scripts     []string `yaml:"scripts,omitempty" json:"scripts,omitempty"`
	Distributed bool     `yaml:"distributed,omitempty" json:"distributed,omitempty"`
}

// PostgresConfig contains fields for postgresql config.
type PostgresConfig struct {
	Username  string     `yaml:"username" json:"username"`
	Password  string     `yaml:"password,omitempty" json:"password,omitempty"`
	Databases []Database `yaml:"databases" json:"databases"`
}

// RedisConfig contains fields for redis config.
type RedisConfig struct {
	Password  string `yaml:"password,omitempty" json:"password"`
	Namespace string `yaml:"namespace" json:"namespace"`
}

// MongodbConfig contains fields for mongodb config.
type MongodbConfig struct {
	Username  string     `yaml:"username" json:"username"`
	Password  string     `yaml:"password,omitempty" json:"password"`
	Databases []Database `yaml:"databases" json:"databases"`
}
